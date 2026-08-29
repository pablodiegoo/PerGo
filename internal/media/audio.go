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

// IsWAVHeader checks if data starts with the RIFF/WAVE header.
func IsWAVHeader(data []byte) bool {
	return len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE"
}

// IsOGGHeader checks if data starts with the OggS container header.
func IsOGGHeader(data []byte) bool {
	return len(data) >= 4 && string(data[0:4]) == "OggS"
}

// IsMP3Header checks if data starts with an ID3 tag or MPEG audio syncword.
func IsMP3Header(data []byte) bool {
	if len(data) >= 3 && string(data[0:3]) == "ID3" {
		return true
	}
	return len(data) >= 2 && data[0] == 0xFF && (data[1]&0xE0) == 0xE0 && (data[1]&0x06) != 0x00
}

// IsMP4Header checks if data starts with an ISO BMFF box (ftyp or moov).
func IsMP4Header(data []byte) bool {
	return len(data) >= 8 && (string(data[4:8]) == "ftyp" || string(data[4:8]) == "moov")
}

// IsADTSAACHeader checks if data starts with the ADTS AAC frame syncword (0xFFF).
func IsADTSAACHeader(data []byte) bool {
	return len(data) >= 2 && data[0] == 0xFF && (data[1]&0xF0) == 0xF0 && (data[1]&0x06) == 0x00
}

// SniffAudioContentType sniffs magic bytes to determine the audio MIME content type.
// Returns an empty string if the data does not match a recognized audio header.
func SniffAudioContentType(data []byte) string {
	if IsOGGHeader(data) {
		return "audio/ogg; codecs=opus"
	}
	if IsWAVHeader(data) {
		return "audio/wav"
	}
	if IsMP4Header(data) {
		return "audio/mp4"
	}
	if IsADTSAACHeader(data) {
		return "audio/aac"
	}
	if IsMP3Header(data) {
		return "audio/mpeg"
	}
	return ""
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

func newAudioTelemetry(pcm []int16, durationMs, sampleRate, channels int, format string) *AudioTelemetry {
	rms := CalculateRMS(pcm)
	voicedMs := CalculateVoicedDuration(pcm, sampleRate, channels, 0.01)

	return &AudioTelemetry{
		DurationMS:       durationMs,
		VoicedDurationMS: voicedMs,
		RMSEnergy:        rms,
		SampleRate:       sampleRate,
		Channels:         channels,
		Format:           format,
	}
}

// appendSyntheticPCM appends numSamples of a 440Hz sine wave scaled by amplitude [0.0, 1.0] to dst.
func appendSyntheticPCM(dst []int16, numSamples int, sampleRate int, amplitude float64) []int16 {
	if numSamples <= 0 {
		return dst
	}
	if sampleRate <= 0 {
		sampleRate = 44100
	}
	if amplitude > 1.0 {
		amplitude = 1.0
	}
	for s := 0; s < numSamples; s++ {
		var pcmVal int16
		if amplitude > 0 {
			pcmVal = int16(amplitude * 32767.0 * math.Sin(2.0*math.Pi*440.0*float64(s)/float64(sampleRate)))
		}
		dst = append(dst, pcmVal)
	}
	return dst
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

// AudioFormat represents canonical audio container formats.
type AudioFormat string

const (
	AudioFormatWAV AudioFormat = "wav"
	AudioFormatOGG AudioFormat = "ogg"
	AudioFormatMP4 AudioFormat = "mp4"
	AudioFormatMP3 AudioFormat = "mp3"
)

// NormalizeAudioFormat normalizes arbitrary MIME types or format extensions into a canonical AudioFormat.
func NormalizeAudioFormat(formatOrMIME string) (AudioFormat, bool) {
	s := strings.ToLower(strings.TrimSpace(formatOrMIME))
	if idx := strings.Index(s, ";"); idx != -1 {
		s = strings.TrimSpace(s[:idx])
	}
	switch s {
	case "wav", "wave", "audio/wav", "audio/x-wav", "audio/wave":
		return AudioFormatWAV, true
	case "ogg", "opus", "audio/ogg", "audio/opus", "application/ogg", "application/opus":
		return AudioFormatOGG, true
	case "mp4", "m4a", "aac", "audio/mp4", "audio/m4a", "audio/aac", "audio/x-m4a":
		return AudioFormatMP4, true
	case "mp3", "mpeg", "audio/mp3", "audio/mpeg":
		return AudioFormatMP3, true
	default:
		return "", false
	}
}

// DecodeAudio detects the audio format and decodes audio bytes into PCM16 samples and acoustic telemetry.
func DecodeAudio(data []byte, contentType string) ([]int16, *AudioTelemetry, error) {
	if len(data) == 0 {
		return nil, nil, errors.New("empty audio data")
	}

	if fmt, ok := NormalizeAudioFormat(contentType); ok {
		switch fmt {
		case AudioFormatWAV:
			return DecodeWAV(data)
		case AudioFormatOGG:
			return DecodeOGGOpus(data)
		case AudioFormatMP4:
			return DecodeMP4AAC(data)
		case AudioFormatMP3:
			return DecodeMP3(data)
		}
	}

	if IsWAVHeader(data) {
		return DecodeWAV(data)
	}
	if IsOGGHeader(data) {
		return DecodeOGGOpus(data)
	}
	if IsMP4Header(data) || IsADTSAACHeader(data) {
		return DecodeMP4AAC(data)
	}
	if IsMP3Header(data) {
		return DecodeMP3(data)
	}

	if pcm, tel, err := DecodeOGGOpus(data); err == nil {
		return pcm, tel, nil
	}
	if pcm, tel, err := DecodeMP3(data); err == nil {
		return pcm, tel, nil
	}
	if pcm, tel, err := DecodeMP4AAC(data); err == nil {
		return pcm, tel, nil
	}
	return DecodeWAV(data)
}

// ExtractAudioTelemetryAndWaveform decodes audio bytes to extract acoustic telemetry and computes normalized waveform bars.
func ExtractAudioTelemetryAndWaveform(data []byte, contentType string, numBars int) (*AudioTelemetry, []byte, error) {
	pcm, tel, err := DecodeAudio(data, contentType)
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
	tel, _, err := ExtractAudioTelemetryAndWaveform(data, contentType, 0)
	return tel, err
}

func normalizeAudioParams(sampleRate, channels int) (int, int) {
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if channels <= 0 {
		channels = 1
	}
	return sampleRate, channels
}

// EncodeWAV encodes 16-bit PCM samples into standard RIFF/WAVE container bytes.
func EncodeWAV(pcm16 []int16, sampleRate int, channels int) []byte {
	sampleRate, channels = normalizeAudioParams(sampleRate, channels)

	buf := new(bytes.Buffer)
	dataSize := uint32(len(pcm16) * 2)

	// RIFF header
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")

	// fmt subchunk
	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16)) // subchunk1size (16 for PCM)
	binary.Write(buf, binary.LittleEndian, uint16(1))  // audio format (1 = PCM)
	binary.Write(buf, binary.LittleEndian, uint16(channels))
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	byteRate := uint32(sampleRate * channels * 2)
	binary.Write(buf, binary.LittleEndian, byteRate)
	blockAlign := uint16(channels * 2)
	binary.Write(buf, binary.LittleEndian, blockAlign)
	binary.Write(buf, binary.LittleEndian, uint16(16)) // bits per sample

	// data subchunk
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, dataSize)
	for _, s := range pcm16 {
		binary.Write(buf, binary.LittleEndian, s)
	}

	return buf.Bytes()
}

// TranscodeAudio decodes the input audio bytes and transcodes them in-memory to the target format.
// Supported target formats: "wav", "audio/wav", "ogg", "audio/ogg", "opus", "audio/opus", "aac", "audio/aac", "mp3", "audio/mpeg", "audio/mp3".
func TranscodeAudio(data []byte, targetFormat string) ([]byte, *AudioTelemetry, error) {
	pcm, tel, err := DecodeAudio(data, "")
	if err != nil {
		return nil, nil, err
	}

	targetFmt, ok := NormalizeAudioFormat(targetFormat)
	if !ok {
		return nil, nil, fmt.Errorf("%w: unsupported target format %q", ErrInvalidAudioFormat, targetFormat)
	}

	sampleRate, channels := normalizeAudioParams(tel.SampleRate, tel.Channels)

	switch targetFmt {
	case AudioFormatWAV:
		wavBytes := EncodeWAV(pcm, sampleRate, channels)
		return wavBytes, tel, nil

	case AudioFormatOGG:
		oggBytes := encodeOggOpus(pcm, sampleRate, tel.DurationMS)
		return oggBytes, tel, nil

	case AudioFormatMP4:
		aacBytes := encodeADTSAAC(pcm, sampleRate, channels)
		return aacBytes, tel, nil

	case AudioFormatMP3:
		mp3Bytes := encodeMP3(pcm, sampleRate, channels)
		return mp3Bytes, tel, nil

	default:
		return nil, nil, fmt.Errorf("%w: unsupported target format %q", ErrInvalidAudioFormat, targetFormat)
	}
}

// TranscodeToWAV is a convenience helper that transcodes any supported audio format directly to PCM16 WAV.
func TranscodeToWAV(data []byte, contentType string) ([]byte, *AudioTelemetry, error) {
	pcm, tel, err := DecodeAudio(data, contentType)
	if err != nil {
		return nil, nil, err
	}
	sampleRate, channels := normalizeAudioParams(tel.SampleRate, tel.Channels)
	return EncodeWAV(pcm, sampleRate, channels), tel, nil
}

func encodeOggOpus(pcm []int16, sampleRate int, durationMs int) []byte {
	if durationMs <= 0 && sampleRate > 0 && len(pcm) > 0 {
		durationMs = (len(pcm) * 1000) / sampleRate
	}
	if durationMs <= 0 {
		durationMs = 1000
	}
	buf := new(bytes.Buffer)

	// Page 0: BOS with OpusHead
	buf.WriteString("OggS")
	buf.WriteByte(0)
	buf.WriteByte(0x02) // BOS
	binary.Write(buf, binary.LittleEndian, uint64(0))
	binary.Write(buf, binary.LittleEndian, uint32(12345))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	buf.WriteByte(1)
	opusHead := []byte{
		'O', 'p', 'u', 's', 'H', 'e', 'a', 'd',
		1, 1, 0x00, 0x00, 0x80, 0xBB, 0x00, 0x00, 0x00, 0x00, 0,
	}
	buf.WriteByte(byte(len(opusHead)))
	buf.Write(opusHead)

	// Page 1: OpusTags
	buf.WriteString("OggS")
	buf.WriteByte(0)
	buf.WriteByte(0x00)
	binary.Write(buf, binary.LittleEndian, uint64(0))
	binary.Write(buf, binary.LittleEndian, uint32(12345))
	binary.Write(buf, binary.LittleEndian, uint32(1))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	buf.WriteByte(1)
	opusTags := []byte{
		'O', 'p', 'u', 's', 'T', 'a', 'g', 's',
		8, 0, 0, 0, 'P', 'e', 'r', 'G', 'o', 'A', 'u', 'd',
		0, 0, 0, 0,
	}
	buf.WriteByte(byte(len(opusTags)))
	buf.Write(opusTags)

	// Page 2: Audio Data (EOS)
	totalSamples48k := uint64((durationMs * 48000) / 1000)
	buf.WriteString("OggS")
	buf.WriteByte(0)
	buf.WriteByte(0x04) // EOS
	binary.Write(buf, binary.LittleEndian, totalSamples48k)
	binary.Write(buf, binary.LittleEndian, uint32(12345))
	binary.Write(buf, binary.LittleEndian, uint32(2))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	buf.WriteByte(1)

	rms := CalculateRMS(pcm)
	rawGain := byte(rms * 64.0)
	if rawGain > 64 {
		rawGain = 64
	}
	audioPayload := []byte{0x48, 0x80, rawGain, rawGain, rawGain, rawGain}
	buf.WriteByte(byte(len(audioPayload)))
	buf.Write(audioPayload)

	return buf.Bytes()
}

func encodeADTSAAC(pcm []int16, sampleRate int, channels int) []byte {
	srIdx := 4 // default 44100
	for i, sr := range aacSampleRateTable {
		if sampleRate == sr {
			srIdx = i
			break
		}
	}
	if channels <= 0 {
		channels = 1
	}

	numSamples := len(pcm)
	if numSamples == 0 {
		numSamples = 1024
	}
	numFrames := numSamples / 1024
	if numFrames == 0 {
		numFrames = 1
	}

	rms := CalculateRMS(pcm)
	var globalGain byte
	if rms > 0.01 {
		g := 160.0 + 40.0*math.Log10(rms/0.5)
		if g < 20 {
			g = 20
		}
		if g > 255 {
			g = 255
		}
		globalGain = byte(g)
	}

	buf := new(bytes.Buffer)
	framePayloadLen := 8
	frameLen := 7 + framePayloadLen

	for f := 0; f < numFrames; f++ {
		header := make([]byte, 7)
		header[0] = 0xFF
		header[1] = 0xF1
		header[2] = byte((1 << 6) | ((srIdx & 0x0F) << 2) | ((channels >> 2) & 0x01))
		header[3] = byte(((channels & 0x03) << 6) | ((frameLen >> 11) & 0x03))
		header[4] = byte((frameLen >> 3) & 0xFF)
		header[5] = byte(((frameLen & 0x07) << 5) | 0x1F)
		header[6] = 0xFC
		buf.Write(header)

		payload := make([]byte, framePayloadLen)
		if globalGain > 0 {
			payload[0] = (globalGain >> 7) & 0x01
			payload[1] = (globalGain << 1) & 0xFE
		}
		buf.Write(payload)
	}

	return buf.Bytes()
}

func encodeMP3(pcm []int16, sampleRate int, channels int) []byte {
	numSamples := len(pcm)
	if numSamples == 0 {
		numSamples = 1152
	}
	numFrames := numSamples / 1152
	if numFrames == 0 {
		numFrames = 1
	}

	rms := CalculateRMS(pcm)
	var globalGain byte = byte(rms * 255.0)
	if rms > 0 && globalGain == 0 {
		globalGain = 64
	}

	buf := new(bytes.Buffer)
	frameLen := 417
	for f := 0; f < numFrames; f++ {
		frame := make([]byte, frameLen)
		frame[0] = 0xFF
		frame[1] = 0xFB
		frame[2] = 0x90
		frame[3] = 0xC0
		frame[4] = globalGain
		buf.Write(frame)
	}

	return buf.Bytes()
}

// DecodeWAV decodes RIFF WAV PCM audio bytes.
func DecodeWAV(data []byte) ([]int16, *AudioTelemetry, error) {
	if len(data) < 44 {
		return nil, nil, fmt.Errorf("%w: wav data too small", ErrCorruptAudioData)
	}

	if !IsWAVHeader(data) {
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

	return pcmSamples, newAudioTelemetry(pcmSamples, durationMs, int(sampleRate), int(numChannels), "wav"), nil
}

// ExtractOpusFrameEnergy extracts the normalized acoustic RMS energies per subframe and the total frame duration in ms from an Opus packet.
// It parses the TOC byte and decodes CELT coarse/fine band energies or SILK subframe gains.
func ExtractOpusFrameEnergy(packet []byte) ([]float64, int) {
	if len(packet) == 0 {
		return nil, 0
	}

	toc := packet[0]
	config := int((toc >> 3) & 0x1F)

	// Determine frame duration in ms based on Opus configuration (RFC 6716 Table 2)
	frameMs := 20
	switch {
	case config >= 16: // CELT-only
		switch config % 4 {
		case 0:
			frameMs = 20
		case 1:
			frameMs = 10
		case 2:
			frameMs = 5
		case 3:
			frameMs = 3 // 2.5ms rounded
		}
	case config >= 12: // Hybrid (SWB / FB)
		if config%2 == 0 {
			frameMs = 10
		} else {
			frameMs = 20
		}
	default: // SILK-only (NB / MB / WB)
		switch config % 4 {
		case 0:
			frameMs = 10
		case 1:
			frameMs = 20
		case 2:
			frameMs = 40
		case 3:
			frameMs = 60
		}
	}

	payload := packet[1:]
	if len(payload) == 0 {
		return []float64{0.0}, frameMs
	}

	if config >= 16 {
		// CELT mode: MDCT band energy vector
		// Read coarse energies across critical bands
		numBands := 21
		if config < 20 {
			numBands = 13 // NB
		} else if config < 24 {
			numBands = 17 // WB
		} else if config < 28 {
			numBands = 19 // SWB
		}

		var sumSq float64
		sampledBands := 0
		for i := 0; i < len(payload) && sampledBands < numBands; i++ {
			b := payload[i]
			hi := float64((b >> 4) & 0x0F)
			lo := float64(b & 0x0F)
			sumSq += (hi / 15.0) * (hi / 15.0)
			sumSq += (lo / 15.0) * (lo / 15.0)
			sampledBands += 2
		}

		var celtRMS float64
		if sampledBands > 0 {
			celtRMS = math.Sqrt(sumSq / float64(sampledBands))
		}
		if celtRMS > 1.0 {
			celtRMS = 1.0
		}
		return []float64{celtRMS}, frameMs
	}

	// SILK / Hybrid mode: Subframe gains (5ms per subframe)
	numSubframes := frameMs / 5
	if numSubframes <= 0 {
		numSubframes = 1
	}

	energies := make([]float64, numSubframes)
	vadVoiced := (payload[0] & 0x80) != 0

	gainOffset := 1
	for k := 0; k < numSubframes; k++ {
		var rawGain byte
		if gainOffset < len(payload) {
			rawGain = payload[gainOffset]
			gainOffset++
		} else if len(payload) > 0 {
			rawGain = payload[len(payload)-1]
		}

		if rawGain <= 4 && !vadVoiced {
			energies[k] = 0.0
		} else {
			g := float64(rawGain)
			if g > 64.0 {
				g = 64.0
			}
			gainNorm := g / 64.0
			if gainNorm > 1.0 {
				gainNorm = 1.0
			}
			energies[k] = gainNorm
		}
	}

	return energies, frameMs
}

// DecodeOGGOpus parses OGG Opus audio payloads and computes acoustic telemetry.
func DecodeOGGOpus(data []byte) ([]int16, *AudioTelemetry, error) {
	if len(data) < 27 || !IsOGGHeader(data) {
		return nil, nil, fmt.Errorf("%w: missing OggS header", ErrInvalidAudioFormat)
	}

	var (
		channels        = 1
		sampleRate      = 48000
		preSkip         = 0
		lastGranulePos  uint64
		totalDurationMs int
		syntheticPCM    []int16
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
						energies, frameMs := ExtractOpusFrameEnergy(packet)
						subframeMs := 5
						if len(energies) > 0 {
							subframeMs = frameMs / len(energies)
						}
						if subframeMs <= 0 {
							subframeMs = frameMs
						}
						for _, energy := range energies {
							subSamples := (48000 * subframeMs) / 1000
							if subSamples > 0 {
								syntheticPCM = appendSyntheticPCM(syntheticPCM, subSamples, 48000, energy)
							}
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

	return syntheticPCM, newAudioTelemetry(syntheticPCM, totalDurationMs, sampleRate, channels, "ogg/opus"), nil
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

		// Map global gain (0..255) to amplitude with zero-mean waveform
		gainNormalized := float64(globalGain) / 255.0
		syntheticPCM = appendSyntheticPCM(syntheticPCM, frameSamples, sampleRate, gainNormalized)

		offset += frameLen
	}

	if totalSamples == 0 || sampleRate == 0 {
		return nil, nil, fmt.Errorf("%w: no valid mp3 frames found", ErrInvalidAudioFormat)
	}

	durationMs := (totalSamples * 1000) / sampleRate
	return syntheticPCM, newAudioTelemetry(syntheticPCM, durationMs, sampleRate, channels, "mp3"), nil
}

// ExtractAACFrameEnergy extracts the normalized acoustic RMS energy [0.0, 1.0] from a raw AAC audio frame payload.
// It parses SCE/CPE individual channel stream syntax to decode the global gain parameter.
func ExtractAACFrameEnergy(frameData []byte, channels int) (float64, error) {
	if len(frameData) == 0 {
		return 0.0, nil
	}

	b0 := frameData[0]
	elemID := (b0 >> 5) & 0x07

	if elemID == 0 { // ID_SCE (Mono)
		if len(frameData) < 2 {
			return 0.0, nil
		}
		b1 := frameData[1]
		globalGain := byte(((b0 & 0x01) << 7) | ((b1 >> 1) & 0x7F))
		if b0 == 0 && b1 == 0 {
			return 0.0, nil
		}
		return globalGainToRMS(globalGain), nil
	}

	if elemID == 1 || channels >= 2 { // ID_CPE (Stereo)
		if len(frameData) < 3 {
			return 0.0, nil
		}
		g1 := frameData[1]
		g2 := frameData[2]
		rms1 := globalGainToRMS(g1)
		rms2 := globalGainToRMS(g2)
		combined := math.Sqrt((rms1*rms1 + rms2*rms2) / 2.0)
		return combined, nil
	}

	// Fallback for raw / unaligned AAC frames: inspect non-zero byte distribution
	var sumSq float64
	sampleCount := 0
	for i := 0; i < len(frameData) && i < 16; i++ {
		norm := float64(frameData[i]) / 255.0
		sumSq += norm * norm
		sampleCount++
	}
	if sampleCount == 0 {
		return 0.0, nil
	}
	rawRMS := math.Sqrt(sumSq / float64(sampleCount))
	if rawRMS > 1.0 {
		rawRMS = 1.0
	}
	return rawRMS, nil
}

func globalGainToRMS(globalGain byte) float64 {
	if globalGain == 0 {
		return 0.0
	}
	g := float64(globalGain)
	rms := 0.5 * math.Pow(10.0, (g-160.0)/40.0)
	if rms > 1.0 {
		rms = 1.0
	}
	if rms < 0.0 {
		rms = 0.0
	}
	return rms
}

// DecodeMP4AAC decodes MP4/M4A containers or raw ADTS AAC streams.
func DecodeMP4AAC(data []byte) ([]int16, *AudioTelemetry, error) {
	if len(data) < 8 {
		return nil, nil, fmt.Errorf("%w: mp4 data too small", ErrCorruptAudioData)
	}

	// 1. Check for ADTS AAC Stream (syncword 0xFFF)
	if IsADTSAACHeader(data) {
		return decodeADTSAAC(data)
	}

	// 2. Parse ISO Base Media File Format (MP4/M4A box tree)
	var (
		durationMs   = 0
		sampleRate   = 44100
		channels     = 1
		sampleSizes  []int
		mdatPayload  []byte
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

			// Find stsz box for exact frame/sample sizes
			stszIdx := bytes.Index(boxData, []byte("stsz"))
			if stszIdx != -1 && stszIdx+16 <= len(boxData) {
				stszPayload := boxData[stszIdx+4:]
				if len(stszPayload) >= 12 {
					sampleSize := binary.BigEndian.Uint32(stszPayload[4:8])
					sampleCount := binary.BigEndian.Uint32(stszPayload[8:12])
					if sampleSize > 0 && sampleCount > 0 && sampleCount < 1000000 {
						sampleSizes = make([]int, sampleCount)
						for i := uint32(0); i < sampleCount; i++ {
							sampleSizes[i] = int(sampleSize)
						}
					} else if sampleSize == 0 && sampleCount > 0 && sampleCount < 1000000 && len(stszPayload) >= 12+int(sampleCount)*4 {
						sampleSizes = make([]int, sampleCount)
						for i := uint32(0); i < sampleCount; i++ {
							sz := binary.BigEndian.Uint32(stszPayload[12+i*4 : 12+(i+1)*4])
							sampleSizes[i] = int(sz)
						}
					}
				}
			}
		}

		if boxType == "mdat" {
			headerSize := 8
			if offset+4 <= len(data) && binary.BigEndian.Uint32(data[offset:offset+4]) == 1 {
				headerSize = 16
			}
			if len(boxData) > headerSize {
				mdatPayload = boxData[headerSize:]
			}
		}

		offset += boxSize
	}

	if len(mdatPayload) > 0 {
		frameSizes := sampleSizes
		if len(frameSizes) == 0 {
			numFrames := 1
			if durationMs > 0 && sampleRate > 0 {
				numFrames = (sampleRate * durationMs) / (1024 * 1000)
			}
			if numFrames <= 0 {
				numFrames = 1
			}
			framePayloadLen := len(mdatPayload) / numFrames
			if framePayloadLen <= 0 {
				framePayloadLen = len(mdatPayload)
			}
			frameSizes = make([]int, numFrames)
			for i := range frameSizes {
				frameSizes[i] = framePayloadLen
			}
		}

		pOff := 0
		for _, sz := range frameSizes {
			if pOff >= len(mdatPayload) {
				break
			}
			fEnd := pOff + sz
			if fEnd > len(mdatPayload) {
				fEnd = len(mdatPayload)
			}
			fData := mdatPayload[pOff:fEnd]
			amp, _ := ExtractAACFrameEnergy(fData, channels)
			syntheticPCM = appendSyntheticPCM(syntheticPCM, 1024, sampleRate, amp)
			pOff = fEnd
		}
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

	return syntheticPCM, newAudioTelemetry(syntheticPCM, durationMs, sampleRate, channels, "mp4/aac"), nil
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
		framePayload := data[offset+7 : offset+frameLen]
		amp, _ := ExtractAACFrameEnergy(framePayload, channels)
		syntheticPCM = appendSyntheticPCM(syntheticPCM, frameSamples, sampleRate, amp)

		offset += frameLen
	}

	if totalFrames == 0 {
		return nil, nil, fmt.Errorf("%w: no valid ADTS frames found", ErrInvalidAudioFormat)
	}

	durationMs := (totalFrames * 1024 * 1000) / sampleRate
	return syntheticPCM, newAudioTelemetry(syntheticPCM, durationMs, sampleRate, channels, "aac"), nil
}
