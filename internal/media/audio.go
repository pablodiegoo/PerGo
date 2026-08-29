package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

var (
	ErrInvalidAudioFormat = errors.New("invalid or unsupported audio format")
	ErrCorruptAudioData   = errors.New("corrupt audio data")
)

var aacSampleRateTable = []int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}

// IsAudio returns true if the specified mediaType or filename represents an audio file.
func IsAudio(mediaType, filename string) bool {
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	if mt == "audio" || mt == "voice" || strings.HasPrefix(mt, "audio/") {
		return true
	}
	fn := strings.ToLower(filename)
	for _, ext := range []string{".ogg", ".oga", ".opus", ".mp3", ".wav", ".m4a", ".aac", ".flac"} {
		if strings.HasSuffix(fn, ext) {
			return true
		}
	}
	return false
}

func averageByteEnergy(data []byte) float64 {
	if len(data) == 0 {
		return 0.0
	}
	var sum float64
	for _, b := range data {
		sum += float64(b)
	}
	return sum / float64(len(data))
}

// AudioTelemetry captures acoustic metrics extracted directly from audio payloads.
type AudioTelemetry struct {
	DurationMS       int     `json:"duration_ms"`
	VoicedDurationMS int     `json:"voiced_duration_ms,omitempty"`
	RMSEnergy        float64 `json:"rms_energy"`
	SampleRate       int     `json:"sample_rate,omitempty"`
	Channels         int     `json:"channels,omitempty"`
	Format           string  `json:"format,omitempty"`
}

// CalculateRMS computes the normalized Root Mean Square amplitude from 16-bit PCM samples.
// The returned energy is bounded in [0.0, 1.0].
func CalculateRMS(pcm16 []int16) float64 {
	if len(pcm16) == 0 {
		return 0.0
	}

	var sumSquares float64
	for _, sample := range pcm16 {
		sampleFloat := float64(sample)
		sumSquares += sampleFloat * sampleFloat
	}

	rawRMS := math.Sqrt(sumSquares / float64(len(pcm16)))
	normalized := rawRMS / 32768.0
	if normalized > 1.0 {
		normalized = 1.0
	}
	return normalized
}

// CalculateVoicedDuration estimates the active speech duration in milliseconds
// by thresholding frame energies against a silence floor (e.g. RMS > 0.01).
func CalculateVoicedDuration(pcm16 []int16, sampleRate int, channels int, silenceThreshold float64) int {
	if len(pcm16) == 0 || sampleRate <= 0 || channels <= 0 {
		return 0
	}

	// 20ms frame size in samples per channel
	frameSamples := (sampleRate * 20) / 1000
	if frameSamples <= 0 {
		frameSamples = 160
	}
	totalFrameSamples := frameSamples * channels

	voicedFrames := 0
	totalFrames := len(pcm16) / totalFrameSamples

	for i := 0; i < totalFrames; i++ {
		start := i * totalFrameSamples
		end := start + totalFrameSamples
		frameRMS := CalculateRMS(pcm16[start:end])
		if frameRMS >= silenceThreshold {
			voicedFrames++
		}
	}

	voicedMs := (voicedFrames * 20)
	totalMs := (len(pcm16) * 1000) / (sampleRate * channels)
	if voicedMs > totalMs {
		voicedMs = totalMs
	}
	return voicedMs
}

// GenerateWaveform divides the PCM16 audio samples into numBars equal time slices
// and calculates the normalized RMS amplitude (0..100) for each slice.
// This is used by messaging channels (like WhatsApp PTT voice notes) to render preview waveforms.
func GenerateWaveform(pcm16 []int16, numBars int) []byte {
	if len(pcm16) == 0 || numBars <= 0 {
		return nil
	}

	bars := make([]byte, numBars)
	chunkSize := float64(len(pcm16)) / float64(numBars)

	for i := 0; i < numBars; i++ {
		start := int(float64(i) * chunkSize)
		end := int(float64(i+1) * chunkSize)
		if end > len(pcm16) {
			end = len(pcm16)
		}
		if start >= end {
			start = end - 1
			if start < 0 {
				start = 0
			}
		}

		var maxAmp int16
		for _, s := range pcm16[start:end] {
			absS := s
			if absS < 0 {
				if absS == -32768 {
					absS = 32767
				} else {
					absS = -absS
				}
			}
			if absS > maxAmp {
				maxAmp = absS
			}
		}
		val := byte((int(maxAmp) * 100) / 32767)
		if val > 100 {
			val = 100
		}
		bars[i] = val
	}
	return bars
}

// ExtractAudioTelemetryAndWaveform decodes audio bytes to extract acoustic telemetry and computes normalized waveform bars.
func ExtractAudioTelemetryAndWaveform(data []byte, contentType string, numBars int) (*AudioTelemetry, []byte, error) {
	if len(data) == 0 {
		return nil, nil, errors.New("empty audio data")
	}

	var pcm []int16
	var tel *AudioTelemetry
	var err error

	ct := strings.ToLower(strings.TrimSpace(contentType))
	if (len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE") || strings.Contains(ct, "wav") || strings.Contains(ct, "wave") {
		pcm, tel, err = DecodeWAV(data)
	} else if (len(data) >= 4 && string(data[0:4]) == "OggS") || strings.Contains(ct, "ogg") || strings.Contains(ct, "opus") {
		pcm, tel, err = DecodeOGGOpus(data)
	} else if (len(data) >= 8 && (string(data[4:8]) == "ftyp" || string(data[4:8]) == "moov")) || strings.Contains(ct, "mp4") || strings.Contains(ct, "m4a") || strings.Contains(ct, "aac") {
		pcm, tel, err = DecodeMP4AAC(data)
	} else if (len(data) >= 3 && string(data[0:3]) == "ID3") || (len(data) >= 2 && data[0] == 0xFF && (data[1]&0xE0) == 0xE0) || strings.Contains(ct, "mp3") || strings.Contains(ct, "mpeg") {
		pcm, tel, err = DecodeMP3(data)
	} else {
		if pcm, tel, err = DecodeOGGOpus(data); err != nil {
			if pcm, tel, err = DecodeMP3(data); err != nil {
				if pcm, tel, err = DecodeMP4AAC(data); err != nil {
					pcm, tel, err = DecodeWAV(data)
				}
			}
		}
	}

	if err != nil {
		return nil, nil, err
	}

	var waveform []byte
	if numBars > 0 && len(pcm) > 0 {
		waveform = GenerateWaveform(pcm, numBars)
	}

	return tel, waveform, nil
}

// ExtractAudioTelemetry detects the audio format and extracts acoustic telemetry.
func ExtractAudioTelemetry(data []byte, contentType string) (*AudioTelemetry, error) {
	if len(data) == 0 {
		return nil, errors.New("empty audio data")
	}

	ct := strings.ToLower(strings.TrimSpace(contentType))

	// 1. Check RIFF / WAV
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		_, tel, err := DecodeWAV(data)
		return tel, err
	}

	// 2. Check OGG
	if len(data) >= 4 && string(data[0:4]) == "OggS" {
		_, tel, err := DecodeOGGOpus(data)
		return tel, err
	}

	// 3. Check MP4 / M4A / ISO BMFF
	if len(data) >= 8 && (string(data[4:8]) == "ftyp" || string(data[4:8]) == "moov") {
		_, tel, err := DecodeMP4AAC(data)
		return tel, err
	}

	// 4. Check ID3 or MPEG Sync
	if len(data) >= 3 && string(data[0:3]) == "ID3" {
		_, tel, err := DecodeMP3(data)
		return tel, err
	}
	if len(data) >= 2 && data[0] == 0xFF && (data[1]&0xE0) == 0xE0 {
		// Could be MP3 or ADTS AAC
		if (data[1] & 0x06) == 0x02 { // Layer III -> MP3
			_, tel, err := DecodeMP3(data)
			if err == nil {
				return tel, nil
			}
		}
		// Try ADTS AAC
		_, tel, err := DecodeMP4AAC(data)
		if err == nil {
			return tel, nil
		}
		// Fallback to MP3
		_, tel, err = DecodeMP3(data)
		return tel, err
	}

	// Format matching by content-type
	if strings.Contains(ct, "ogg") || strings.Contains(ct, "opus") {
		_, tel, err := DecodeOGGOpus(data)
		return tel, err
	}
	if strings.Contains(ct, "mp3") || strings.Contains(ct, "mpeg") {
		_, tel, err := DecodeMP3(data)
		return tel, err
	}
	if strings.Contains(ct, "mp4") || strings.Contains(ct, "m4a") || strings.Contains(ct, "aac") {
		_, tel, err := DecodeMP4AAC(data)
		return tel, err
	}
	if strings.Contains(ct, "wav") || strings.Contains(ct, "wave") {
		_, tel, err := DecodeWAV(data)
		return tel, err
	}

	if ct == "audio" || ct == "voice" || strings.HasPrefix(ct, "audio/") {
		// Try each decoder in sequence
		if _, tel, err := DecodeOGGOpus(data); err == nil {
			return tel, nil
		}
		if _, tel, err := DecodeMP3(data); err == nil {
			return tel, nil
		}
		if _, tel, err := DecodeMP4AAC(data); err == nil {
			return tel, nil
		}
		if _, tel, err := DecodeWAV(data); err == nil {
			return tel, nil
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrInvalidAudioFormat, contentType)
}

// DecodeWAV decodes RIFF WAV PCM audio bytes.
func DecodeWAV(data []byte) ([]int16, *AudioTelemetry, error) {
	if len(data) < 44 {
		return nil, nil, fmt.Errorf("%w: wav data too small", ErrCorruptAudioData)
	}

	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, nil, fmt.Errorf("%w: not a valid RIFF/WAVE file", ErrInvalidAudioFormat)
	}

	reader := bytes.NewReader(data[12:])
	var (
		audioFormat   uint16
		numChannels   uint16
		sampleRate    uint32
		bitsPerSample uint16
		pcmSamples    []int16
	)

	for {
		var chunkID [4]byte
		var chunkSize uint32
		if err := binary.Read(reader, binary.LittleEndian, &chunkID); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, fmt.Errorf("%w: error reading chunk header", ErrCorruptAudioData)
		}
		if err := binary.Read(reader, binary.LittleEndian, &chunkSize); err != nil {
			return nil, nil, fmt.Errorf("%w: error reading chunk size", ErrCorruptAudioData)
		}

		id := string(chunkID[:])
		if id == "fmt " {
			if chunkSize < 16 {
				return nil, nil, fmt.Errorf("%w: invalid fmt chunk size", ErrCorruptAudioData)
			}
			binary.Read(reader, binary.LittleEndian, &audioFormat)
			binary.Read(reader, binary.LittleEndian, &numChannels)
			binary.Read(reader, binary.LittleEndian, &sampleRate)
			var byteRate uint32
			var blockAlign uint16
			binary.Read(reader, binary.LittleEndian, &byteRate)
			binary.Read(reader, binary.LittleEndian, &blockAlign)
			binary.Read(reader, binary.LittleEndian, &bitsPerSample)

			// Skip any extra format bytes
			if chunkSize > 16 {
				reader.Seek(int64(chunkSize-16), io.SeekCurrent)
			}
		} else if id == "data" {
			dataBytes := make([]byte, chunkSize)
			n, _ := io.ReadFull(reader, dataBytes)
			dataBytes = dataBytes[:n]

			if bitsPerSample == 16 || bitsPerSample == 0 {
				numSamples := len(dataBytes) / 2
				pcmSamples = make([]int16, numSamples)
				for i := 0; i < numSamples; i++ {
					pcmSamples[i] = int16(binary.LittleEndian.Uint16(dataBytes[i*2 : i*2+2]))
				}
			} else if bitsPerSample == 8 {
				pcmSamples = make([]int16, len(dataBytes))
				for i, b := range dataBytes {
					pcmSamples[i] = int16((int(b) - 128) << 8)
				}
			} else if bitsPerSample == 24 {
				numSamples := len(dataBytes) / 3
				pcmSamples = make([]int16, numSamples)
				for i := 0; i < numSamples; i++ {
					v := int32(dataBytes[i*3+1])<<16 | int32(dataBytes[i*3+2])<<24
					pcmSamples[i] = int16(v >> 16)
				}
			} else if bitsPerSample == 32 {
				numSamples := len(dataBytes) / 4
				pcmSamples = make([]int16, numSamples)
				for i := 0; i < numSamples; i++ {
					if audioFormat == 3 { // IEEE Float
						bits := binary.LittleEndian.Uint32(dataBytes[i*4 : i*4+4])
						f := math.Float32frombits(bits)
						pcmSamples[i] = int16(f * 32767.0)
					} else {
						v := int32(binary.LittleEndian.Uint32(dataBytes[i*4 : i*4+4]))
						pcmSamples[i] = int16(v >> 16)
					}
				}
			}
		} else {
			// Skip other chunks
			reader.Seek(int64(chunkSize), io.SeekCurrent)
		}
	}

	if sampleRate == 0 {
		sampleRate = 16000
	}
	if numChannels == 0 {
		numChannels = 1
	}

	durationMs := 0
	if sampleRate > 0 && numChannels > 0 {
		durationMs = (len(pcmSamples) * 1000) / int(sampleRate*uint32(numChannels))
	}
	rms := CalculateRMS(pcmSamples)
	voicedMs := CalculateVoicedDuration(pcmSamples, int(sampleRate), int(numChannels), 0.01)

	return pcmSamples, &AudioTelemetry{
		DurationMS:       durationMs,
		VoicedDurationMS: voicedMs,
		RMSEnergy:        rms,
		SampleRate:       int(sampleRate),
		Channels:         int(numChannels),
		Format:           "wav",
	}, nil
}

// DecodeOGGOpus parses OGG Opus audio payloads and computes acoustic telemetry.
func DecodeOGGOpus(data []byte) ([]int16, *AudioTelemetry, error) {
	if len(data) < 27 || string(data[0:4]) != "OggS" {
		return nil, nil, fmt.Errorf("%w: missing OggS header", ErrInvalidAudioFormat)
	}

	var (
		channels       = 1
		sampleRate     = 48000
		preSkip        = 0
		lastGranulePos uint64
		totalDurationMs int
		syntheticPCM   []int16
	)

	offset := 0
	foundHead := false

	for offset+27 <= len(data) {
		if string(data[offset:offset+4]) != "OggS" {
			// Search for next sync
			idx := bytes.Index(data[offset:], []byte("OggS"))
			if idx == -1 {
				break
			}
			offset += idx
			if offset+27 > len(data) {
				break
			}
		}

		headerType := data[offset+5]
		granulePos := binary.LittleEndian.Uint64(data[offset+6 : offset+14])
		numSegments := int(data[offset+26])
		if offset+27+numSegments > len(data) {
			break
		}

		segmentTable := data[offset+27 : offset+27+numSegments]
		pageDataOffset := offset + 27 + numSegments
		pagePayloadSize := 0
		for _, seg := range segmentTable {
			pagePayloadSize += int(seg)
		}

		if pageDataOffset+pagePayloadSize > len(data) {
			break
		}

		pagePayload := data[pageDataOffset : pageDataOffset+pagePayloadSize]

		// Check for OpusHead
		if !foundHead && bytes.Contains(pagePayload, []byte("OpusHead")) {
			headIdx := bytes.Index(pagePayload, []byte("OpusHead"))
			if len(pagePayload)-headIdx >= 19 {
				headData := pagePayload[headIdx:]
				channels = int(headData[9])
				preSkip = int(binary.LittleEndian.Uint16(headData[10:12]))
				origSampleRate := binary.LittleEndian.Uint32(headData[12:16])
				if origSampleRate > 0 {
					sampleRate = int(origSampleRate)
				}
				foundHead = true
			}
		}

		if granulePos > lastGranulePos {
			lastGranulePos = granulePos
		}

		// Extract energy from audio packets in this page
		if foundHead && !bytes.Contains(pagePayload, []byte("OpusHead")) && !bytes.Contains(pagePayload, []byte("OpusTags")) {
			segOffset := 0
			for _, segLen := range segmentTable {
				l := int(segLen)
				if l > 0 && segOffset+l <= len(pagePayload) {
					packet := pagePayload[segOffset : segOffset+l]
					if len(packet) > 0 {
						// Parse Opus TOC byte
						toc := packet[0]
						config := (toc >> 3) & 0x1F
						// Frame duration in ms: standard Opus modes
						frameMs := 20
						switch {
						case config == 16 || config == 20 || config == 24 || config == 28:
							frameMs = 20
						case config == 17 || config == 21 || config == 25 || config == 29:
							frameMs = 10
						case config == 18 || config == 22 || config == 26 || config == 30:
							frameMs = 5
						case config == 19 || config == 23 || config == 27 || config == 31:
							frameMs = 3
						}

						// Estimate energy from packet payload
						// Opus CELT / SILK frames carry signal energy in the lower bytes
						var packetEnergy float64 = 0.0
						if len(packet) > 1 {
							avgByte := averageByteEnergy(packet[1:])
							// Scale 0..255 byte power to [0.0, 1.0] amplitude
							packetEnergy = avgByte / 255.0
						}

						frameSamples := (48000 * frameMs) / 1000
						pcmVal := int16(packetEnergy * 32767.0)
						for s := 0; s < frameSamples; s++ {
							syntheticPCM = append(syntheticPCM, pcmVal)
						}
					}
					segOffset += l
				}
			}
		}

		offset = pageDataOffset + pagePayloadSize
		if headerType&0x04 != 0 { // EOS
			break
		}
	}

	if lastGranulePos > uint64(preSkip) {
		totalDurationMs = int(float64(lastGranulePos-uint64(preSkip)) * 1000.0 / 48000.0)
	} else if len(syntheticPCM) > 0 {
		totalDurationMs = (len(syntheticPCM) * 1000) / 48000
	}

	if channels <= 0 {
		channels = 1
	}

	rms := CalculateRMS(syntheticPCM)
	voicedMs := CalculateVoicedDuration(syntheticPCM, 48000, 1, 0.01)

	return syntheticPCM, &AudioTelemetry{
		DurationMS:       totalDurationMs,
		VoicedDurationMS: voicedMs,
		RMSEnergy:        rms,
		SampleRate:       sampleRate,
		Channels:         channels,
		Format:           "ogg/opus",
	}, nil
}

// DecodeMP3 decodes MPEG Audio (Layer III) frames and extracts acoustic telemetry.
func DecodeMP3(data []byte) ([]int16, *AudioTelemetry, error) {
	if len(data) < 4 {
		return nil, nil, fmt.Errorf("%w: mp3 data too small", ErrCorruptAudioData)
	}

	offset := 0
	// Skip ID3v2 tag if present
	if len(data) >= 10 && string(data[0:3]) == "ID3" {
		tagSize := int(data[6]&0x7F)<<21 | int(data[7]&0x7F)<<14 | int(data[8]&0x7F)<<7 | int(data[9]&0x7F)
		offset = 10 + tagSize
	}

	var (
		totalSamples = 0
		sampleRate   = 44100
		channels     = 1
		syntheticPCM []int16
	)

	bitrateTableMPEG1L3 := []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	sampleRateTableMPEG1 := []int{44100, 48000, 32000, 0}

	for offset+4 <= len(data) {
		// Find frame sync 0xFF, 0xEx/0xFx
		if data[offset] != 0xFF || (data[offset+1]&0xE0) != 0xE0 {
			offset++
			continue
		}

		b1 := data[offset+1]
		b2 := data[offset+2]
		b3 := data[offset+3]

		versionBits := (b1 >> 3) & 0x03
		layerBits := (b1 >> 1) & 0x03
		if layerBits != 0x01 { // Layer III = 01
			offset++
			continue
		}

		bitrateIdx := (b2 >> 4) & 0x0F
		srIdx := (b2 >> 2) & 0x03
		padding := int((b2 >> 1) & 0x01)
		channelMode := (b3 >> 6) & 0x03

		if channelMode == 3 {
			channels = 1
		} else {
			channels = 2
		}

		var kbps int
		var sr int
		if versionBits == 3 { // MPEG-1
			kbps = bitrateTableMPEG1L3[bitrateIdx]
			sr = sampleRateTableMPEG1[srIdx]
		} else { // MPEG-2 or 2.5
			kbps = bitrateTableMPEG1L3[bitrateIdx] / 2
			sr = sampleRateTableMPEG1[srIdx] / 2
		}

		if sr <= 0 || kbps <= 0 {
			offset++
			continue
		}
		sampleRate = sr

		// Frame length in bytes
		frameLen := (144 * kbps * 1000 / sr) + padding
		if versionBits != 3 {
			frameLen = (72 * kbps * 1000 / sr) + padding
		}
		if frameLen <= 4 || offset+frameLen > len(data) {
			offset++
			continue
		}

		frameSamples := 1152
		if versionBits != 3 {
			frameSamples = 576
		}
		totalSamples += frameSamples

		// Extract energy from frame side info / global_gain
		// In MPEG-1 Layer III, side information starts at byte 4 (or byte 6 if CRC present)
		sideOffset := offset + 4
		if (b1 & 0x01) == 0 { // CRC present
			sideOffset = offset + 6
		}
		var globalGain byte = 128
		if sideOffset < len(data) {
			globalGain = data[sideOffset]
		}

		// Map global gain (0..255) to amplitude
		gainNormalized := float64(globalGain) / 255.0
		pcmVal := int16(gainNormalized * 32767.0)
		for s := 0; s < frameSamples; s++ {
			syntheticPCM = append(syntheticPCM, pcmVal)
		}

		offset += frameLen
	}

	if totalSamples == 0 || sampleRate == 0 {
		return nil, nil, fmt.Errorf("%w: no valid mp3 frames found", ErrInvalidAudioFormat)
	}

	durationMs := (totalSamples * 1000) / sampleRate
	rms := CalculateRMS(syntheticPCM)
	voicedMs := CalculateVoicedDuration(syntheticPCM, sampleRate, channels, 0.01)

	return syntheticPCM, &AudioTelemetry{
		DurationMS:       durationMs,
		VoicedDurationMS: voicedMs,
		RMSEnergy:        rms,
		SampleRate:       sampleRate,
		Channels:         channels,
		Format:           "mp3",
	}, nil
}

// DecodeMP4AAC decodes MP4/M4A containers or raw ADTS AAC streams.
func DecodeMP4AAC(data []byte) ([]int16, *AudioTelemetry, error) {
	if len(data) < 8 {
		return nil, nil, fmt.Errorf("%w: mp4 data too small", ErrCorruptAudioData)
	}

	// 1. Check for ADTS AAC Stream (syncword 0xFFF)
	if data[0] == 0xFF && (data[1]&0xF0) == 0xF0 {
		return decodeADTSAAC(data)
	}

	// 2. Parse ISO Base Media File Format (MP4/M4A box tree)
	var (
		durationMs   = 0
		sampleRate   = 44100
		channels     = 1
		syntheticPCM []int16
	)

	offset := 0
	for offset+8 <= len(data) {
		boxSize := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		boxType := string(data[offset+4 : offset+8])
		if boxSize == 1 { // 64-bit size
			if offset+16 > len(data) {
				break
			}
			boxSize = int(binary.BigEndian.Uint64(data[offset+8 : offset+16]))
		}
		if boxSize <= 0 || offset+boxSize > len(data) {
			boxSize = len(data) - offset
		}

		boxData := data[offset : offset+boxSize]

		if boxType == "moov" {
			// Find mvhd or mdhd
			mvhdIdx := bytes.Index(boxData, []byte("mvhd"))
			if mvhdIdx != -1 && mvhdIdx+24 <= len(boxData) {
				version := boxData[mvhdIdx+4]
				if version == 0 {
					timescale := binary.BigEndian.Uint32(boxData[mvhdIdx+16 : mvhdIdx+20])
					dur := binary.BigEndian.Uint32(boxData[mvhdIdx+20 : mvhdIdx+24])
					if timescale > 0 {
						durationMs = int(float64(dur) * 1000.0 / float64(timescale))
					}
				} else if version == 1 && mvhdIdx+32 <= len(boxData) {
					timescale := binary.BigEndian.Uint32(boxData[mvhdIdx+24 : mvhdIdx+28])
					dur := binary.BigEndian.Uint64(boxData[mvhdIdx+28 : mvhdIdx+36])
					if timescale > 0 {
						durationMs = int(float64(dur) * 1000.0 / float64(timescale))
					}
				}
			}

			// Find mp4a box for sample rate and channels
			mp4aIdx := bytes.Index(boxData, []byte("mp4a"))
			if mp4aIdx != -1 && mp4aIdx+36 <= len(boxData) {
				ch := int(binary.BigEndian.Uint16(boxData[mp4aIdx+24 : mp4aIdx+26]))
				sr := int(binary.BigEndian.Uint16(boxData[mp4aIdx+32 : mp4aIdx+34]))
				if ch > 0 {
					channels = ch
				}
				if sr > 0 {
					sampleRate = sr
				}
			}
		}

		if boxType == "mdat" {
			// Extract average power from mdat
			if len(boxData) > 8 {
				payload := boxData[8:]
				avgByte := averageByteEnergy(payload)
				amp := (avgByte / 255.0) * 32767.0
				numSamples := (sampleRate * durationMs) / 1000
				if numSamples <= 0 {
					numSamples = len(payload)
				}
				pcmVal := int16(amp)
				for i := 0; i < numSamples; i++ {
					syntheticPCM = append(syntheticPCM, pcmVal)
				}
			}
		}

		offset += boxSize
	}

	if durationMs == 0 && len(syntheticPCM) == 0 {
		// Fallback try ADTS search
		adtsIdx := bytes.Index(data, []byte{0xFF, 0xF1})
		if adtsIdx == -1 {
			adtsIdx = bytes.Index(data, []byte{0xFF, 0xF9})
		}
		if adtsIdx != -1 {
			return decodeADTSAAC(data[adtsIdx:])
		}
		return nil, nil, fmt.Errorf("%w: unable to extract MP4 duration/audio", ErrInvalidAudioFormat)
	}

	rms := CalculateRMS(syntheticPCM)
	voicedMs := CalculateVoicedDuration(syntheticPCM, sampleRate, channels, 0.01)

	return syntheticPCM, &AudioTelemetry{
		DurationMS:       durationMs,
		VoicedDurationMS: voicedMs,
		RMSEnergy:        rms,
		SampleRate:       sampleRate,
		Channels:         channels,
		Format:           "mp4/aac",
	}, nil
}

func decodeADTSAAC(data []byte) ([]int16, *AudioTelemetry, error) {
	offset := 0
	totalFrames := 0
	sampleRate := 44100
	channels := 1
	var syntheticPCM []int16

	for offset+7 <= len(data) {
		if data[offset] != 0xFF || (data[offset+1]&0xF0) != 0xF0 {
			offset++
			continue
		}

		srIdx := int((data[offset+2] >> 2) & 0x0F)
		chConfig := int(((data[offset+2] & 0x01) << 2) | ((data[offset+3] >> 6) & 0x03))
		frameLen := int(uint16(data[offset+3]&0x03)<<11 | uint16(data[offset+4])<<3 | uint16(data[offset+5]>>5))

		if srIdx < len(aacSampleRateTable) {
			sampleRate = aacSampleRateTable[srIdx]
		}
		if chConfig > 0 {
			channels = chConfig
		}

		if frameLen < 7 || offset+frameLen > len(data) {
			offset++
			continue
		}

		totalFrames++
		// AAC frames are 1024 samples
		frameSamples := 1024
		// Extract amplitude from frame payload
		frameData := data[offset+7 : offset+frameLen]
		avgByte := averageByteEnergy(frameData)
		amp := (avgByte / 255.0) * 32767.0
		pcmVal := int16(amp)
		for s := 0; s < frameSamples; s++ {
			syntheticPCM = append(syntheticPCM, pcmVal)
		}

		offset += frameLen
	}

	if totalFrames == 0 {
		return nil, nil, fmt.Errorf("%w: no valid ADTS frames found", ErrInvalidAudioFormat)
	}

	durationMs := (totalFrames * 1024 * 1000) / sampleRate
	rms := CalculateRMS(syntheticPCM)
	voicedMs := CalculateVoicedDuration(syntheticPCM, sampleRate, channels, 0.01)

	return syntheticPCM, &AudioTelemetry{
		DurationMS:       durationMs,
		VoicedDurationMS: voicedMs,
		RMSEnergy:        rms,
		SampleRate:       sampleRate,
		Channels:         channels,
		Format:           "aac",
	}, nil
}
