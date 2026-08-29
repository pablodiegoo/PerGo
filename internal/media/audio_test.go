package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

// Helper to generate a 16-bit PCM mono sine wave
func generateSinePCM16(sampleRate int, freq float64, durationSec float64, amplitude float64) []int16 {
	numSamples := int(float64(sampleRate) * durationSec)
	samples := make([]int16, numSamples)
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		val := amplitude * math.Sin(2*math.Pi*freq*t)
		if val > 32767 {
			val = 32767
		} else if val < -32768 {
			val = -32768
		}
		samples[i] = int16(val)
	}
	return samples
}

// Helper to build a valid RIFF/WAV file from int16 PCM samples
func buildWAVBytes(samples []int16, sampleRate int, channels int) []byte {
	buf := new(bytes.Buffer)
	dataSize := uint32(len(samples) * 2)

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
	for _, s := range samples {
		binary.Write(buf, binary.LittleEndian, s)
	}

	return buf.Bytes()
}

// Helper to build a minimal valid Ogg Opus container
func buildOggOpusBytes(durationMs int, sampleRate int) []byte {
	buf := new(bytes.Buffer)

	// Page 0: BOS with OpusHead
	// OggS header
	buf.WriteString("OggS")
	buf.WriteByte(0)                                      // version
	buf.WriteByte(0x02)                                   // header_type: BOS
	binary.Write(buf, binary.LittleEndian, uint64(0))     // granule_pos = 0
	binary.Write(buf, binary.LittleEndian, uint32(12345)) // serial
	binary.Write(buf, binary.LittleEndian, uint32(0))     // seq = 0
	binary.Write(buf, binary.LittleEndian, uint32(0))     // checksum
	buf.WriteByte(1)                                      // 1 segment
	opusHead := []byte{
		'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', // Magic
		1,          // Version
		1,          // Channels = 1
		0x00, 0x00, // Pre-skip = 0
		0x80, 0xBB, 0x00, 0x00, // Input sample rate = 48000
		0x00, 0x00, // Output gain = 0
		0, // Mapping family = 0
	}
	buf.WriteByte(byte(len(opusHead))) // segment size
	buf.Write(opusHead)

	// Page 1: OpusTags
	buf.WriteString("OggS")
	buf.WriteByte(0)
	buf.WriteByte(0x00)
	binary.Write(buf, binary.LittleEndian, uint64(0))
	binary.Write(buf, binary.LittleEndian, uint32(12345))
	binary.Write(buf, binary.LittleEndian, uint32(1)) // seq = 1
	binary.Write(buf, binary.LittleEndian, uint32(0))
	buf.WriteByte(1)
	opusTags := []byte{
		'O', 'p', 'u', 's', 'T', 'a', 'g', 's',
		8, 0, 0, 0, 'P', 'e', 'r', 'G', 'o', 'A', 'u', 'd',
		0, 0, 0, 0, // comment count = 0
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
	binary.Write(buf, binary.LittleEndian, uint32(2)) // seq = 2
	binary.Write(buf, binary.LittleEndian, uint32(0))
	buf.WriteByte(1)
	// Opus audio frame: CELT mono 20ms packet with medium energy
	// TOC byte: config 20 (CELT FB 20ms) -> (20 << 3) = 0xA0
	audioPayload := []byte{0xA0, 0x7F, 0x50, 0x30, 0x20, 0x10}
	buf.WriteByte(byte(len(audioPayload)))
	buf.Write(audioPayload)

	return buf.Bytes()
}

// Helper to build a minimal MP3 with valid MPEG-1 Layer 3 frames
func buildMP3Bytes(numFrames int) []byte {
	buf := new(bytes.Buffer)

	// ID3v2.3 tag (10 bytes header + padding)
	buf.WriteString("ID3")
	buf.WriteByte(3) // version 2.3
	buf.WriteByte(0) // revision
	buf.WriteByte(0) // flags
	// Size synchsafe int: 10 bytes tag body
	buf.Write([]byte{0x00, 0x00, 0x00, 0x0A})
	buf.Write(make([]byte, 10)) // 10 bytes empty body

	// MPEG-1 Layer 3 Frame Header:
	// Sync: 11 bits (0xFFE / 0xFFF)
	// Byte 0: 0xFF
	// Byte 1: 0xFB -> MPEG-1 (bits 4-3 = 11), Layer III (bits 2-1 = 01), No CRC (bit 0 = 1)
	// Byte 2: 0x90 -> Bitrate index 9 = 128 kbps (bits 7-4 = 1001), 44100 Hz (bits 3-2 = 00), Padding = 0, Priv = 0
	// Byte 3: 0xC0 -> Channel mode = Single channel / Mono (bits 7-6 = 11)
	// Frame size for 128kbps, 44100Hz = 144 * 128000 / 44100 = 417 bytes
	frameLen := 417
	for i := 0; i < numFrames; i++ {
		frame := make([]byte, frameLen)
		frame[0] = 0xFF
		frame[1] = 0xFB
		frame[2] = 0x90
		frame[3] = 0xC0
		// Side info: global_gain = 160 (medium amplitude)
		frame[4] = 0xA0
		frame[5] = 0x50
		buf.Write(frame)
	}

	return buf.Bytes()
}

// Helper to build a minimal ISO BMFF / MP4 container with audio metadata
func buildMP4AACBytes(durationMs int, sampleRate int) []byte {
	buf := new(bytes.Buffer)

	// 1. ftyp box
	ftyp := new(bytes.Buffer)
	ftyp.WriteString("M4A ")
	binary.Write(ftyp, binary.BigEndian, uint32(0)) // minor version
	ftyp.WriteString("M4A mp42isom")
	writeBox(buf, "ftyp", ftyp.Bytes())

	// 2. moov box
	moov := new(bytes.Buffer)

	// mvhd box
	mvhd := new(bytes.Buffer)
	mvhd.WriteByte(0)                               // version
	mvhd.Write([]byte{0, 0, 0})                     // flags
	binary.Write(mvhd, binary.BigEndian, uint32(0)) // creation_time
	binary.Write(mvhd, binary.BigEndian, uint32(0)) // modification_time
	timescale := uint32(1000)
	binary.Write(mvhd, binary.BigEndian, timescale)
	binary.Write(mvhd, binary.BigEndian, uint32(durationMs)) // duration
	binary.Write(mvhd, binary.BigEndian, uint32(0x00010000)) // rate 1.0
	binary.Write(mvhd, binary.BigEndian, uint16(0x0100))     // volume 1.0
	mvhd.Write(make([]byte, 10))                             // reserved
	// Matrix (36 bytes unity matrix)
	for i := 0; i < 9; i++ {
		if i == 0 || i == 4 || i == 8 {
			binary.Write(mvhd, binary.BigEndian, uint32(0x00010000))
		} else {
			binary.Write(mvhd, binary.BigEndian, uint32(0))
		}
	}
	mvhd.Write(make([]byte, 24))                    // pre_defined
	binary.Write(mvhd, binary.BigEndian, uint32(2)) // next_track_id
	writeBox(moov, "mvhd", mvhd.Bytes())

	// trak box
	trak := new(bytes.Buffer)
	// mdia box
	mdia := new(bytes.Buffer)
	// mdhd box
	mdhd := new(bytes.Buffer)
	mdhd.WriteByte(0)
	mdhd.Write([]byte{0, 0, 0})
	binary.Write(mdhd, binary.BigEndian, uint32(0))
	binary.Write(mdhd, binary.BigEndian, uint32(0))
	binary.Write(mdhd, binary.BigEndian, uint32(sampleRate))
	mediaDuration := uint32((durationMs * sampleRate) / 1000)
	binary.Write(mdhd, binary.BigEndian, mediaDuration)
	binary.Write(mdhd, binary.BigEndian, uint16(0x55C4)) // language
	binary.Write(mdhd, binary.BigEndian, uint16(0))      // quality
	writeBox(mdia, "mdhd", mdhd.Bytes())

	// hdlr box
	hdlr := new(bytes.Buffer)
	hdlr.Write(make([]byte, 8))
	hdlr.WriteString("soun") // sound media
	hdlr.Write(make([]byte, 12))
	writeBox(mdia, "hdlr", hdlr.Bytes())

	// minf -> stbl -> stsd (mp4a)
	minf := new(bytes.Buffer)
	stbl := new(bytes.Buffer)
	stsd := new(bytes.Buffer)
	stsd.Write(make([]byte, 4))                     // version + flags
	binary.Write(stsd, binary.BigEndian, uint32(1)) // 1 entry
	// mp4a audio entry
	mp4a := new(bytes.Buffer)
	mp4a.Write(make([]byte, 6))                              // reserved
	binary.Write(mp4a, binary.BigEndian, uint16(1))          // data_reference_index
	mp4a.Write(make([]byte, 8))                              // sound version/revision/vendor
	binary.Write(mp4a, binary.BigEndian, uint16(1))          // channel_count = 1
	binary.Write(mp4a, binary.BigEndian, uint16(16))         // sample_size = 16
	mp4a.Write(make([]byte, 4))                              // compression_id/packet_size
	binary.Write(mp4a, binary.BigEndian, uint16(sampleRate)) // sample_rate
	mp4a.Write(make([]byte, 2))
	writeBox(stsd, "mp4a", mp4a.Bytes())
	writeBox(stbl, "stsd", stsd.Bytes())
	writeBox(minf, "stbl", stbl.Bytes())
	writeBox(mdia, "minf", minf.Bytes())
	writeBox(trak, "mdia", mdia.Bytes())
	writeBox(moov, "trak", trak.Bytes())

	writeBox(buf, "moov", moov.Bytes())

	// 3. mdat box with mock audio frames
	mdatData := make([]byte, 256)
	for i := range mdatData {
		mdatData[i] = byte(i % 128)
	}
	writeBox(buf, "mdat", mdatData)

	return buf.Bytes()
}

func writeBox(w *bytes.Buffer, boxType string, payload []byte) {
	size := uint32(8 + len(payload))
	binary.Write(w, binary.BigEndian, size)
	w.WriteString(boxType)
	w.Write(payload)
}

func TestAppendSyntheticPCM(t *testing.T) {
	t.Run("zero or negative numSamples returns original slice", func(t *testing.T) {
		orig := []int16{1, 2, 3}
		res := appendSyntheticPCM(orig, 0, 44100, 0.5)
		if len(res) != 3 {
			t.Fatalf("expected len 3, got %d", len(res))
		}
		res = appendSyntheticPCM(orig, -10, 44100, 0.5)
		if len(res) != 3 {
			t.Fatalf("expected len 3, got %d", len(res))
		}
	})

	t.Run("zero or negative amplitude produces zero samples", func(t *testing.T) {
		res := appendSyntheticPCM(nil, 100, 44100, 0.0)
		if len(res) != 100 {
			t.Fatalf("expected len 100, got %d", len(res))
		}
		for i, v := range res {
			if v != 0 {
				t.Fatalf("expected 0 at %d, got %d", i, v)
			}
		}
		res = appendSyntheticPCM(nil, 50, 44100, -0.2)
		if len(res) != 50 {
			t.Fatalf("expected len 50, got %d", len(res))
		}
		for i, v := range res {
			if v != 0 {
				t.Fatalf("expected 0 at %d, got %d", i, v)
			}
		}
	})

	t.Run("zero or negative sampleRate defaults to 44100 without panic", func(t *testing.T) {
		res := appendSyntheticPCM(nil, 100, 0, 0.5)
		if len(res) != 100 {
			t.Fatalf("expected len 100, got %d", len(res))
		}
	})

	t.Run("amplitude > 1.0 clamped and generates zero-mean sine wave", func(t *testing.T) {
		sampleRate := 48000
		numSamples := sampleRate // 1 full second
		res := appendSyntheticPCM(nil, numSamples, sampleRate, 1.5)
		if len(res) != numSamples {
			t.Fatalf("expected len %d, got %d", numSamples, len(res))
		}

		rms := CalculateRMS(res)
		expectedRMS := 1.0 / math.Sqrt(2) // ~0.7071
		if math.Abs(rms-expectedRMS) > 0.01 {
			t.Errorf("expected RMS ~%f, got %f", expectedRMS, rms)
		}

		// Verify zero-mean (DC offset close to 0)
		var sum float64
		for _, s := range res {
			sum += float64(s)
		}
		mean := sum / float64(len(res))
		if math.Abs(mean) > 10.0 {
			t.Errorf("expected zero-mean wave, got mean = %f", mean)
		}
	})
}

func TestAudioHeaderPredicates(t *testing.T) {
	t.Run("IsWAVHeader", func(t *testing.T) {
		valid := []byte("RIFF\x24\x00\x00\x00WAVEfmt ")
		if !IsWAVHeader(valid) {
			t.Errorf("expected true for valid RIFF/WAVE header")
		}
		if IsWAVHeader([]byte("RIFF1234AVI ")) {
			t.Errorf("expected false for RIFF AVI")
		}
		if IsWAVHeader([]byte("RIFF")) {
			t.Errorf("expected false for short slice")
		}
		if IsWAVHeader(nil) {
			t.Errorf("expected false for nil")
		}
	})

	t.Run("IsOGGHeader", func(t *testing.T) {
		valid := []byte("OggS\x00\x02")
		if !IsOGGHeader(valid) {
			t.Errorf("expected true for valid OggS header")
		}
		if IsOGGHeader([]byte("OggX")) {
			t.Errorf("expected false for OggX")
		}
		if IsOGGHeader([]byte("Ogg")) {
			t.Errorf("expected false for short slice")
		}
		if IsOGGHeader(nil) {
			t.Errorf("expected false for nil")
		}
	})

	t.Run("IsMP3Header", func(t *testing.T) {
		id3 := []byte("ID3\x03\x00\x00\x00\x00\x00\x00")
		if !IsMP3Header(id3) {
			t.Errorf("expected true for ID3 tag")
		}
		syncFB := []byte{0xFF, 0xFB, 0x90, 0x64}
		if !IsMP3Header(syncFB) {
			t.Errorf("expected true for 0xFF 0xFB frame sync")
		}
		syncFA := []byte{0xFF, 0xFA, 0x90, 0x64}
		if !IsMP3Header(syncFA) {
			t.Errorf("expected true for 0xFF 0xFA frame sync")
		}
		if IsMP3Header([]byte{0xFF, 0x00}) {
			t.Errorf("expected false for 0xFF 0x00")
		}
		if IsMP3Header([]byte{0x00, 0xFF}) {
			t.Errorf("expected false for 0x00 0xFF")
		}
		if IsMP3Header([]byte("ID")) {
			t.Errorf("expected false for short slice")
		}
		if IsMP3Header(nil) {
			t.Errorf("expected false for nil")
		}
	})

	t.Run("IsMP4Header", func(t *testing.T) {
		ftyp := []byte("\x00\x00\x00\x18ftypisom")
		if !IsMP4Header(ftyp) {
			t.Errorf("expected true for ftyp box")
		}
		moov := []byte("\x00\x00\x00\x20moov....")
		if !IsMP4Header(moov) {
			t.Errorf("expected true for moov box")
		}
		if IsMP4Header([]byte("\x00\x00\x00\x18freeisom")) {
			t.Errorf("expected false for free box")
		}
		if IsMP4Header([]byte("ftyp")) {
			t.Errorf("expected false for short slice")
		}
		if IsMP4Header(nil) {
			t.Errorf("expected false for nil")
		}
	})

	t.Run("IsADTSAACHeader", func(t *testing.T) {
		syncF1 := []byte{0xFF, 0xF1, 0x50, 0x80}
		if !IsADTSAACHeader(syncF1) {
			t.Errorf("expected true for 0xFF 0xF1 ADTS sync")
		}
		syncF9 := []byte{0xFF, 0xF9, 0x50, 0x80}
		if !IsADTSAACHeader(syncF9) {
			t.Errorf("expected true for 0xFF 0xF9 ADTS sync")
		}
		if IsADTSAACHeader([]byte{0xFF, 0x00}) {
			t.Errorf("expected false for 0xFF 0x00")
		}
		if IsADTSAACHeader([]byte{0xFF}) {
			t.Errorf("expected false for 1 byte slice")
		}
		if IsADTSAACHeader(nil) {
			t.Errorf("expected false for nil")
		}
	})
}

func TestSniffAudioContentType(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{name: "OGG Opus", data: []byte("OggS\x00\x02"), expected: "audio/ogg; codecs=opus"},
		{name: "RIFF WAV", data: []byte("RIFF\x24\x00\x00\x00WAVEfmt "), expected: "audio/wav"},
		{name: "MP3 ID3", data: []byte("ID3\x03\x00\x00\x00\x00\x00\x00"), expected: "audio/mpeg"},
		{name: "MP3 Sync", data: []byte{0xFF, 0xFB, 0x90, 0x64}, expected: "audio/mpeg"},
		{name: "MP4 FTYP", data: []byte("\x00\x00\x00\x18ftypisom"), expected: "audio/mp4"},
		{name: "MP4 MOOV", data: []byte("\x00\x00\x00\x20moov...."), expected: "audio/mp4"},
		{name: "ADTS AAC", data: []byte{0xFF, 0xF1, 0x50, 0x80}, expected: "audio/aac"},
		{name: "Unknown plain text", data: []byte("hello world"), expected: ""},
		{name: "Empty slice", data: []byte{}, expected: ""},
		{name: "Nil slice", data: nil, expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SniffAudioContentType(tt.data)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestCalculateRMS(t *testing.T) {
	t.Run("empty slice returns 0.0", func(t *testing.T) {
		if rms := CalculateRMS(nil); rms != 0.0 {
			t.Errorf("expected 0.0, got %f", rms)
		}
		if rms := CalculateRMS([]int16{}); rms != 0.0 {
			t.Errorf("expected 0.0, got %f", rms)
		}
	})

	t.Run("all zeros silence returns 0.0", func(t *testing.T) {
		silence := make([]int16, 16000)
		if rms := CalculateRMS(silence); rms != 0.0 {
			t.Errorf("expected 0.0 for silence, got %f", rms)
		}
	})

	t.Run("full scale DC returns 1.0", func(t *testing.T) {
		fullScale := make([]int16, 1000)
		for i := range fullScale {
			fullScale[i] = 32767
		}
		rms := CalculateRMS(fullScale)
		if math.Abs(rms-0.999969) > 0.001 {
			t.Errorf("expected ~1.0, got %f", rms)
		}
	})

	t.Run("full scale sine wave RMS is ~0.707", func(t *testing.T) {
		sine := generateSinePCM16(48000, 440, 1.0, 32767)
		rms := CalculateRMS(sine)
		expected := 1.0 / math.Sqrt(2) // ~0.7071
		if math.Abs(rms-expected) > 0.01 {
			t.Errorf("expected ~%f, got %f", expected, rms)
		}
	})

	t.Run("half scale sine wave RMS is ~0.3535", func(t *testing.T) {
		sine := generateSinePCM16(48000, 440, 1.0, 16384)
		rms := CalculateRMS(sine)
		expected := 0.5 / math.Sqrt(2) // ~0.3535
		if math.Abs(rms-expected) > 0.01 {
			t.Errorf("expected ~%f, got %f", expected, rms)
		}
	})
}

func TestGenerateWaveform(t *testing.T) {
	t.Run("empty slice or zero bars returns nil", func(t *testing.T) {
		if wf := GenerateWaveform(nil, 64); wf != nil {
			t.Errorf("expected nil for nil pcm, got %v", wf)
		}
		if wf := GenerateWaveform([]int16{100}, 0); wf != nil {
			t.Errorf("expected nil for 0 bars, got %v", wf)
		}
	})

	t.Run("generates exact number of bars bounded in 0..100", func(t *testing.T) {
		sine := generateSinePCM16(48000, 440, 1.0, 32767)
		wf := GenerateWaveform(sine, 64)
		if len(wf) != 64 {
			t.Fatalf("expected 64 bars, got %d", len(wf))
		}
		for i, b := range wf {
			if b > 100 {
				t.Errorf("bar %d exceeds 100: %d", i, b)
			}
		}
	})

	t.Run("silence produces all zero bars", func(t *testing.T) {
		silence := make([]int16, 16000)
		wf := GenerateWaveform(silence, 32)
		if len(wf) != 32 {
			t.Fatalf("expected 32 bars, got %d", len(wf))
		}
		for i, b := range wf {
			if b != 0 {
				t.Errorf("expected 0 for silence bar %d, got %d", i, b)
			}
		}
	})
}

func TestDecodeWAV(t *testing.T) {
	sampleRate := 16000
	channels := 1
	sine := generateSinePCM16(sampleRate, 440, 1.5, 20000) // 1.5 seconds = 1500 ms
	wavBytes := buildWAVBytes(sine, sampleRate, channels)

	samples, telemetry, err := DecodeWAV(wavBytes)
	if err != nil {
		t.Fatalf("DecodeWAV failed: %v", err)
	}

	if len(samples) != len(sine) {
		t.Errorf("expected %d samples, got %d", len(sine), len(samples))
	}
	if telemetry.DurationMS != 1500 {
		t.Errorf("expected DurationMS = 1500, got %d", telemetry.DurationMS)
	}
	if telemetry.SampleRate != 16000 {
		t.Errorf("expected SampleRate = 16000, got %d", telemetry.SampleRate)
	}
	if telemetry.Channels != 1 {
		t.Errorf("expected Channels = 1, got %d", telemetry.Channels)
	}
	if telemetry.RMSEnergy <= 0.0 || telemetry.RMSEnergy > 1.0 {
		t.Errorf("invalid RMSEnergy: %f", telemetry.RMSEnergy)
	}
	if telemetry.Format != "wav" {
		t.Errorf("expected Format = wav, got %s", telemetry.Format)
	}
}

func TestDecodeOGGOpus(t *testing.T) {
	durationMs := 2500
	oggBytes := buildOggOpusBytes(durationMs, 48000)

	samples, telemetry, err := DecodeOGGOpus(oggBytes)
	if err != nil {
		t.Fatalf("DecodeOGGOpus failed: %v", err)
	}

	if telemetry.DurationMS != 2500 {
		t.Errorf("expected DurationMS = 2500, got %d", telemetry.DurationMS)
	}
	if telemetry.SampleRate != 48000 {
		t.Errorf("expected SampleRate = 48000, got %d", telemetry.SampleRate)
	}
	if telemetry.Channels != 1 {
		t.Errorf("expected Channels = 1, got %d", telemetry.Channels)
	}
	if telemetry.RMSEnergy < 0.0 || telemetry.RMSEnergy > 1.0 {
		t.Errorf("invalid RMSEnergy: %f", telemetry.RMSEnergy)
	}
	if telemetry.Format != "ogg/opus" {
		t.Errorf("expected Format = ogg/opus, got %s", telemetry.Format)
	}
	_ = samples
}

func TestDecodeMP3(t *testing.T) {
	// 50 frames of 1152 samples @ 44100 Hz = 50 * (1152 / 44100) = ~1.306s = 1306 ms
	mp3Bytes := buildMP3Bytes(50)

	samples, telemetry, err := DecodeMP3(mp3Bytes)
	if err != nil {
		t.Fatalf("DecodeMP3 failed: %v", err)
	}

	if telemetry.DurationMS <= 1200 || telemetry.DurationMS >= 1400 {
		t.Errorf("expected DurationMS ~1306, got %d", telemetry.DurationMS)
	}
	if telemetry.SampleRate != 44100 {
		t.Errorf("expected SampleRate = 44100, got %d", telemetry.SampleRate)
	}
	if telemetry.RMSEnergy < 0.0 || telemetry.RMSEnergy > 1.0 {
		t.Errorf("invalid RMSEnergy: %f", telemetry.RMSEnergy)
	}
	if telemetry.Format != "mp3" {
		t.Errorf("expected Format = mp3, got %s", telemetry.Format)
	}
	_ = samples
}

func TestDecodeMP4AAC(t *testing.T) {
	durationMs := 3200
	mp4Bytes := buildMP4AACBytes(durationMs, 44100)

	samples, telemetry, err := DecodeMP4AAC(mp4Bytes)
	if err != nil {
		t.Fatalf("DecodeMP4AAC failed: %v", err)
	}

	if telemetry.DurationMS != 3200 {
		t.Errorf("expected DurationMS = 3200, got %d", telemetry.DurationMS)
	}
	if telemetry.SampleRate != 44100 {
		t.Errorf("expected SampleRate = 44100, got %d", telemetry.SampleRate)
	}
	if telemetry.RMSEnergy < 0.0 || telemetry.RMSEnergy > 1.0 {
		t.Errorf("invalid RMSEnergy: %f", telemetry.RMSEnergy)
	}
	if telemetry.Format != "mp4/aac" {
		t.Errorf("expected Format = mp4/aac, got %s", telemetry.Format)
	}
	_ = samples
}

func TestExtractAudioTelemetry(t *testing.T) {
	t.Run("extracts from Ogg Opus payload", func(t *testing.T) {
		oggBytes := buildOggOpusBytes(1800, 48000)
		telemetry, err := ExtractAudioTelemetry(oggBytes, "audio/ogg; codecs=opus")
		if err != nil {
			t.Fatalf("ExtractAudioTelemetry failed: %v", err)
		}
		if telemetry.DurationMS != 1800 {
			t.Errorf("expected DurationMS = 1800, got %d", telemetry.DurationMS)
		}
	})

	t.Run("extracts from MP3 payload", func(t *testing.T) {
		mp3Bytes := buildMP3Bytes(20)
		telemetry, err := ExtractAudioTelemetry(mp3Bytes, "audio/mpeg")
		if err != nil {
			t.Fatalf("ExtractAudioTelemetry failed: %v", err)
		}
		if telemetry.DurationMS <= 0 {
			t.Errorf("expected positive DurationMS, got %d", telemetry.DurationMS)
		}
	})

	t.Run("extracts from MP4 payload", func(t *testing.T) {
		mp4Bytes := buildMP4AACBytes(2400, 48000)
		telemetry, err := ExtractAudioTelemetry(mp4Bytes, "audio/mp4")
		if err != nil {
			t.Fatalf("ExtractAudioTelemetry failed: %v", err)
		}
		if telemetry.DurationMS != 2400 {
			t.Errorf("expected DurationMS = 2400, got %d", telemetry.DurationMS)
		}
	})

	t.Run("extracts from WAV payload", func(t *testing.T) {
		wavBytes := buildWAVBytes(generateSinePCM16(16000, 440, 1.0, 10000), 16000, 1)
		telemetry, err := ExtractAudioTelemetry(wavBytes, "audio/wav")
		if err != nil {
			t.Fatalf("ExtractAudioTelemetry failed: %v", err)
		}
		if telemetry.DurationMS != 1000 {
			t.Errorf("expected DurationMS = 1000, got %d", telemetry.DurationMS)
		}
	})

	t.Run("returns error on non-audio", func(t *testing.T) {
		_, err := ExtractAudioTelemetry([]byte("plain text not audio"), "image/jpeg")
		if err == nil {
			t.Errorf("expected error for non-audio media type, got nil")
		}
	})
}

func TestIsAudio(t *testing.T) {
	tests := []struct {
		mediaType string
		filename  string
		expected  bool
	}{
		{"audio", "", true},
		{"voice", "", true},
		{"audio/ogg", "", true},
		{"audio/mpeg", "", true},
		{"audio/mp4", "", true},
		{"AUDIO/WAV", "", true},
		{"", "voice_note.ogg", true},
		{"", "audio.opus", true},
		{"", "song.mp3", true},
		{"", "track.wav", true},
		{"", "music.m4a", true},
		{"", "sound.aac", true},
		{"", "clip.flac", true},
		{"", "sample.oga", true},
		{"image/png", "picture.png", false},
		{"video/mp4", "movie.mp4", false},
		{"application/pdf", "doc.pdf", false},
		{"", "file.txt", false},
		{"", "", false},
	}

	for _, tt := range tests {
		got := IsAudio(tt.mediaType, tt.filename)
		if got != tt.expected {
			t.Errorf("IsAudio(%q, %q) = %v, expected %v", tt.mediaType, tt.filename, got, tt.expected)
		}
	}
}

func TestCalculateVoicedDuration_EdgeCases(t *testing.T) {
	t.Run("empty slice or zero sample rate or channels", func(t *testing.T) {
		if d := CalculateVoicedDuration(nil, 16000, 1, 0.01); d != 0 {
			t.Errorf("expected 0, got %d", d)
		}
		if d := CalculateVoicedDuration([]int16{1, 2, 3}, 0, 1, 0.01); d != 0 {
			t.Errorf("expected 0, got %d", d)
		}
		if d := CalculateVoicedDuration([]int16{1, 2, 3}, 16000, 0, 0.01); d != 0 {
			t.Errorf("expected 0, got %d", d)
		}
		// sampleRate so low frameSamples would be 0
		if d := CalculateVoicedDuration([]int16{1, 2, 3}, 10, 1, 0.01); d != 0 {
			t.Errorf("expected 0, got %d", d)
		}
	})

	t.Run("voiced duration does not exceed total duration", func(t *testing.T) {
		sine := generateSinePCM16(16000, 440, 0.05, 30000) // 50ms
		d := CalculateVoicedDuration(sine, 16000, 1, 0.001)
		if d > 50 {
			t.Errorf("expected voicedMs <= 50, got %d", d)
		}
	})
}

func TestGenerateWaveform_EdgeCases(t *testing.T) {
	t.Run("negative minimum int16 sample clamping", func(t *testing.T) {
		samples := []int16{-32768, -32768, -32768}
		wf := GenerateWaveform(samples, 3)
		if len(wf) != 3 {
			t.Fatalf("expected 3 bars, got %d", len(wf))
		}
		for i, b := range wf {
			if b != 100 {
				t.Errorf("expected 100 for min int16, got %d at %d", b, i)
			}
		}
	})

	t.Run("numBars greater than samples", func(t *testing.T) {
		samples := []int16{10000, -10000}
		wf := GenerateWaveform(samples, 10)
		if len(wf) != 10 {
			t.Fatalf("expected 10 bars, got %d", len(wf))
		}
	})
}

func TestExtractAudioTelemetryAndWaveform_EdgeCases(t *testing.T) {
	t.Run("empty audio data returns error", func(t *testing.T) {
		_, _, err := ExtractAudioTelemetryAndWaveform(nil, "audio/ogg", 10)
		if err == nil {
			t.Errorf("expected error on nil data, got nil")
		}
	})

	t.Run("generates waveform with positive numBars", func(t *testing.T) {
		wavBytes := buildWAVBytes(generateSinePCM16(16000, 440, 1.0, 10000), 16000, 1)
		tel, wf, err := ExtractAudioTelemetryAndWaveform(wavBytes, "", 32)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tel == nil || len(wf) != 32 {
			t.Errorf("expected 32 waveform bars, got %d", len(wf))
		}
	})
}

func buildWAVMultiBitDepth(bitsPerSample int, audioFormat int, numSamples int, sampleRate int, channels int, includeExtraChunk bool) []byte {
	buf := new(bytes.Buffer)
	bytesPerSample := bitsPerSample / 8
	dataSize := uint32(numSamples * channels * bytesPerSample)
	totalSize := uint32(36 + dataSize)
	if includeExtraChunk {
		totalSize += 16
	}

	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, totalSize)
	buf.WriteString("WAVE")

	// Extra chunk before fmt
	if includeExtraChunk {
		buf.WriteString("JUNK")
		binary.Write(buf, binary.LittleEndian, uint32(8))
		buf.Write([]byte("12345678"))
	}

	// fmt chunk
	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(18)) // 18 bytes (with 2 extra format bytes)
	binary.Write(buf, binary.LittleEndian, uint16(audioFormat))
	binary.Write(buf, binary.LittleEndian, uint16(channels))
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	byteRate := uint32(sampleRate * channels * bytesPerSample)
	binary.Write(buf, binary.LittleEndian, byteRate)
	blockAlign := uint16(channels * bytesPerSample)
	binary.Write(buf, binary.LittleEndian, blockAlign)
	binary.Write(buf, binary.LittleEndian, uint16(bitsPerSample))
	binary.Write(buf, binary.LittleEndian, uint16(0)) // 2 extra format bytes

	// data chunk
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, dataSize)

	for i := 0; i < numSamples*channels; i++ {
		switch bitsPerSample {
		case 8:
			buf.WriteByte(byte(128 + (i % 64)))
		case 24:
			buf.WriteByte(0x00)
			buf.WriteByte(0x40)
			buf.WriteByte(0x20)
		case 32:
			if audioFormat == 3 { // IEEE float
				var f float32 = 0.5
				binary.Write(buf, binary.LittleEndian, math.Float32bits(f))
			} else {
				var val int32 = 10000000
				binary.Write(buf, binary.LittleEndian, val)
			}
		}
	}

	return buf.Bytes()
}

func TestDecodeWAV_EdgeCases(t *testing.T) {
	t.Run("too small data", func(t *testing.T) {
		_, _, err := DecodeWAV([]byte("RIFF1234"))
		if err == nil {
			t.Errorf("expected error for small data")
		}
	})

	t.Run("invalid header", func(t *testing.T) {
		_, _, err := DecodeWAV(make([]byte, 50))
		if err == nil {
			t.Errorf("expected error for non-RIFF")
		}
	})

	t.Run("invalid fmt chunk size", func(t *testing.T) {
		buf := new(bytes.Buffer)
		buf.WriteString("RIFF")
		binary.Write(buf, binary.LittleEndian, uint32(40))
		buf.WriteString("WAVE")
		buf.WriteString("fmt ")
		binary.Write(buf, binary.LittleEndian, uint32(8)) // invalid < 16
		buf.Write(make([]byte, 8))
		_, _, err := DecodeWAV(buf.Bytes())
		if err == nil {
			t.Errorf("expected error for invalid fmt chunk size")
		}
	})

	t.Run("truncated chunk header", func(t *testing.T) {
		buf := new(bytes.Buffer)
		buf.WriteString("RIFF")
		binary.Write(buf, binary.LittleEndian, uint32(40))
		buf.WriteString("WAVE")
		buf.WriteString("fm") // truncated chunk header
		_, _, err := DecodeWAV(buf.Bytes())
		if err == nil {
			t.Errorf("expected error for truncated chunk header")
		}
	})

	t.Run("truncated chunk size", func(t *testing.T) {
		buf := new(bytes.Buffer)
		buf.WriteString("RIFF")
		binary.Write(buf, binary.LittleEndian, uint32(40))
		buf.WriteString("WAVE")
		buf.WriteString("fmt ")
		buf.Write([]byte{0x01}) // only 1 byte of size
		_, _, err := DecodeWAV(buf.Bytes())
		if err == nil {
			t.Errorf("expected error for truncated chunk size")
		}
	})

	t.Run("8-bit PCM format", func(t *testing.T) {
		wav := buildWAVMultiBitDepth(8, 1, 100, 8000, 1, true)
		pcm, tel, err := DecodeWAV(wav)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pcm) != 100 || tel.SampleRate != 8000 {
			t.Errorf("unexpected 8-bit decode result: len=%d sr=%d", len(pcm), tel.SampleRate)
		}
	})

	t.Run("24-bit PCM format", func(t *testing.T) {
		wav := buildWAVMultiBitDepth(24, 1, 100, 24000, 1, false)
		pcm, tel, err := DecodeWAV(wav)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pcm) != 100 || tel.SampleRate != 24000 {
			t.Errorf("unexpected 24-bit decode result: len=%d sr=%d", len(pcm), tel.SampleRate)
		}
	})

	t.Run("32-bit integer PCM format", func(t *testing.T) {
		wav := buildWAVMultiBitDepth(32, 1, 100, 48000, 1, false)
		pcm, tel, err := DecodeWAV(wav)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pcm) != 100 || tel.SampleRate != 48000 {
			t.Errorf("unexpected 32-bit int decode result: len=%d sr=%d", len(pcm), tel.SampleRate)
		}
	})

	t.Run("32-bit IEEE float PCM format", func(t *testing.T) {
		wav := buildWAVMultiBitDepth(32, 3, 100, 44100, 1, false)
		pcm, tel, err := DecodeWAV(wav)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pcm) != 100 || tel.SampleRate != 44100 {
			t.Errorf("unexpected 32-bit float decode result: len=%d sr=%d", len(pcm), tel.SampleRate)
		}
	})
}

func TestDecodeOGGOpus_EdgeCases(t *testing.T) {
	t.Run("too small data or invalid header", func(t *testing.T) {
		_, _, err := DecodeOGGOpus([]byte("OggS123"))
		if err == nil {
			t.Errorf("expected error for small OGG data")
		}
		_, _, err = DecodeOGGOpus(make([]byte, 30))
		if err == nil {
			t.Errorf("expected error for missing OggS header")
		}
	})

	t.Run("multiple Opus frame duration configs", func(t *testing.T) {
		buf := new(bytes.Buffer)
		// BOS OpusHead
		buf.WriteString("OggS")
		buf.WriteByte(0)
		buf.WriteByte(0x02)
		binary.Write(buf, binary.LittleEndian, uint64(0))
		binary.Write(buf, binary.LittleEndian, uint32(100))
		binary.Write(buf, binary.LittleEndian, uint32(0))
		binary.Write(buf, binary.LittleEndian, uint32(0))
		buf.WriteByte(1)
		opusHead := []byte{
			'O', 'p', 'u', 's', 'H', 'e', 'a', 'd',
			1, 1, 0x00, 0x00, 0x80, 0xBB, 0x00, 0x00, 0x00, 0x00, 0,
		}
		buf.WriteByte(byte(len(opusHead)))
		buf.Write(opusHead)

		// Page 1: Audio packets with different TOC configs (17=10ms, 18=5ms, 19=3ms, 16=20ms)
		configs := []byte{17 << 3, 18 << 3, 19 << 3, 16 << 3}
		buf.WriteString("OggS")
		buf.WriteByte(0)
		buf.WriteByte(0x04) // EOS
		binary.Write(buf, binary.LittleEndian, uint64(4800))
		binary.Write(buf, binary.LittleEndian, uint32(100))
		binary.Write(buf, binary.LittleEndian, uint32(1))
		binary.Write(buf, binary.LittleEndian, uint32(0))
		buf.WriteByte(byte(len(configs)))
		for range configs {
			buf.WriteByte(6) // 6 bytes packet
		}
		for _, cfg := range configs {
			buf.Write([]byte{cfg, 0x01, 0x02, 0x03, 0x04, 0x05})
		}

		samples, tel, err := DecodeOGGOpus(buf.Bytes())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tel.SampleRate != 48000 || len(samples) == 0 {
			t.Errorf("unexpected decode result: len=%d sr=%d", len(samples), tel.SampleRate)
		}
	})

	t.Run("sync search resynchronization", func(t *testing.T) {
		validOgg := buildOggOpusBytes(1000, 48000)
		corrupted := append([]byte("junkdata"), validOgg...)
		_, _, err := DecodeOGGOpus(corrupted)
		if err == nil {
			t.Errorf("expected error on corrupted start")
		}
	})
}

func TestDecodeMP3_EdgeCases(t *testing.T) {
	t.Run("too small data", func(t *testing.T) {
		_, _, err := DecodeMP3([]byte{0xFF, 0xFB})
		if err == nil {
			t.Errorf("expected error for small mp3")
		}
	})

	t.Run("no valid mp3 frames", func(t *testing.T) {
		_, _, err := DecodeMP3([]byte("some random text with no sync"))
		if err == nil {
			t.Errorf("expected error for invalid mp3")
		}
	})

	t.Run("MPEG-2 Layer 3 with CRC and Stereo", func(t *testing.T) {
		buf := new(bytes.Buffer)
		// MPEG-2 (versionBits = 2), Layer III (layerBits = 1), CRC present (bit 0 = 0) -> 0xF2
		// Bitrate 64kbps (bitrateIdx = 7), 22050Hz (srIdx = 0), Padding = 1 -> 0x72
		// Stereo channel mode = 0 (channelMode = 0) -> 0x00
		frameLen := (72 * 64 * 1000 / 22050) + 1
		for i := 0; i < 5; i++ {
			frame := make([]byte, frameLen)
			frame[0] = 0xFF
			frame[1] = 0xF2
			frame[2] = 0x72
			frame[3] = 0x00
			frame[6] = 0x90 // side info global_gain with CRC
			buf.Write(frame)
		}

		samples, tel, err := DecodeMP3(buf.Bytes())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tel.Channels != 2 || tel.SampleRate != 22050 || len(samples) == 0 {
			t.Errorf("unexpected result: ch=%d sr=%d len=%d", tel.Channels, tel.SampleRate, len(samples))
		}
	})
}

func buildADTSAACBytes(numFrames int, srIdx int, chConfig int) []byte {
	buf := new(bytes.Buffer)
	framePayloadLen := 30
	frameLen := 7 + framePayloadLen

	for i := 0; i < numFrames; i++ {
		header := make([]byte, 7)
		header[0] = 0xFF
		header[1] = 0xF1 // sync 0xFFF, MPEG-4, Layer 0, No CRC
		header[2] = byte((1 << 6) | ((srIdx & 0x0F) << 2) | ((chConfig >> 2) & 0x01))
		header[3] = byte(((chConfig & 0x03) << 6) | ((frameLen >> 11) & 0x03))
		header[4] = byte((frameLen >> 3) & 0xFF)
		header[5] = byte(((frameLen & 0x07) << 5) | 0x1F)
		header[6] = 0xFC

		buf.Write(header)
		payload := make([]byte, framePayloadLen)
		for p := range payload {
			payload[p] = byte(p * 5)
		}
		buf.Write(payload)
	}

	return buf.Bytes()
}

func TestDecodeADTSAAC(t *testing.T) {
	t.Run("valid ADTS stream", func(t *testing.T) {
		adtsData := buildADTSAACBytes(10, 4, 2) // 44100Hz (srIdx=4), Stereo (chConfig=2)
		samples, tel, err := decodeADTSAAC(adtsData)
		if err != nil {
			t.Fatalf("decodeADTSAAC failed: %v", err)
		}
		if tel.Channels != 2 || tel.SampleRate != 44100 || len(samples) == 0 {
			t.Errorf("unexpected ADTS decode: ch=%d sr=%d len=%d", tel.Channels, tel.SampleRate, len(samples))
		}
		if tel.Format != "aac" {
			t.Errorf("expected Format=aac, got %s", tel.Format)
		}
	})

	t.Run("delegation from DecodeMP4AAC", func(t *testing.T) {
		adtsData := buildADTSAACBytes(5, 3, 1) // 48000Hz (srIdx=3)
		samples, tel, err := DecodeMP4AAC(adtsData)
		if err != nil {
			t.Fatalf("DecodeMP4AAC failed on ADTS: %v", err)
		}
		if tel.SampleRate != 48000 || len(samples) == 0 {
			t.Errorf("expected 48000Hz, got %d", tel.SampleRate)
		}
	})

	t.Run("no valid ADTS frames", func(t *testing.T) {
		_, _, err := decodeADTSAAC([]byte("invalid adts stream data"))
		if err == nil {
			t.Errorf("expected error on invalid ADTS")
		}
	})
}

func TestDecodeMP4AAC_EdgeCases(t *testing.T) {
	t.Run("too small data", func(t *testing.T) {
		_, _, err := DecodeMP4AAC([]byte("mp4"))
		if err == nil {
			t.Errorf("expected error for small mp4")
		}
	})

	t.Run("mvhd version 1 and 64-bit box size", func(t *testing.T) {
		buf := new(bytes.Buffer)

		// 1. ftyp box
		ftypPayload := []byte("M4A \x00\x00\x00\x00M4A mp42isom")
		writeBox(buf, "ftyp", ftypPayload)

		// 2. moov box
		moov := new(bytes.Buffer)

		// mvhd version 1
		mvhd := new(bytes.Buffer)
		mvhd.WriteByte(1) // version 1
		mvhd.Write([]byte{0, 0, 0})
		binary.Write(mvhd, binary.BigEndian, uint64(0))          // creation_time (64-bit)
		binary.Write(mvhd, binary.BigEndian, uint64(0))          // mod_time (64-bit)
		binary.Write(mvhd, binary.BigEndian, uint32(1000))       // timescale
		binary.Write(mvhd, binary.BigEndian, uint64(2000))       // duration (64-bit)
		binary.Write(mvhd, binary.BigEndian, uint32(0x00010000)) // rate
		binary.Write(mvhd, binary.BigEndian, uint16(0x0100))     // volume
		mvhd.Write(make([]byte, 10))
		for i := 0; i < 9; i++ {
			if i == 0 || i == 4 || i == 8 {
				binary.Write(mvhd, binary.BigEndian, uint32(0x00010000))
			} else {
				binary.Write(mvhd, binary.BigEndian, uint32(0))
			}
		}
		mvhd.Write(make([]byte, 24))
		binary.Write(mvhd, binary.BigEndian, uint32(2))
		writeBox(moov, "mvhd", mvhd.Bytes())

		writeBox(buf, "moov", moov.Bytes())

		// mdat with 64-bit box header (size = 1)
		mdatPayload := make([]byte, 64)
		binary.Write(buf, binary.BigEndian, uint32(1)) // 64-bit indicator
		buf.WriteString("mdat")
		binary.Write(buf, binary.BigEndian, uint64(16+len(mdatPayload)))
		buf.Write(mdatPayload)

		samples, tel, err := DecodeMP4AAC(buf.Bytes())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tel.DurationMS != 2000 || len(samples) == 0 {
			t.Errorf("unexpected result: dur=%d len=%d", tel.DurationMS, len(samples))
		}
	})

	t.Run("unsupported empty MP4 triggers ADTS fallback search", func(t *testing.T) {
		buf := new(bytes.Buffer)
		writeBox(buf, "ftyp", []byte("mp42"))
		// append valid ADTS frame
		adtsFrame := buildADTSAACBytes(1, 4, 1)
		buf.Write(adtsFrame)

		samples, tel, err := DecodeMP4AAC(buf.Bytes())
		if err != nil {
			t.Fatalf("expected ADTS fallback to succeed: %v", err)
		}
		if len(samples) == 0 || tel.Format != "aac" {
			t.Errorf("expected aac format, got %s", tel.Format)
		}
	})

	t.Run("empty invalid MP4 container returns error", func(t *testing.T) {
		buf := new(bytes.Buffer)
		writeBox(buf, "ftyp", []byte("mp42"))
		_, _, err := DecodeMP4AAC(buf.Bytes())
		if err == nil {
			t.Errorf("expected error for empty MP4 container")
		}
	})

	t.Run("MP4 container with stsz sample size table", func(t *testing.T) {
		buf := new(bytes.Buffer)
		writeBox(buf, "ftyp", []byte("mp42\x00\x00\x00\x00mp42isom"))

		moov := new(bytes.Buffer)
		// mvhd
		mvhd := new(bytes.Buffer)
		mvhd.WriteByte(0)
		mvhd.Write([]byte{0, 0, 0})
		binary.Write(mvhd, binary.BigEndian, uint32(0))
		binary.Write(mvhd, binary.BigEndian, uint32(0))
		binary.Write(mvhd, binary.BigEndian, uint32(44100))
		binary.Write(mvhd, binary.BigEndian, uint32(44100)) // 1s
		mvhd.Write(make([]byte, 24))
		binary.Write(mvhd, binary.BigEndian, uint32(2))
		writeBox(moov, "mvhd", mvhd.Bytes())

		// stsz table with 2 frames (sizes: 10, 14 bytes)
		stsz := new(bytes.Buffer)
		stsz.WriteByte(0)
		stsz.Write([]byte{0, 0, 0})
		binary.Write(stsz, binary.BigEndian, uint32(0)) // variable sample size
		binary.Write(stsz, binary.BigEndian, uint32(2)) // 2 samples
		binary.Write(stsz, binary.BigEndian, uint32(10))
		binary.Write(stsz, binary.BigEndian, uint32(14))
		writeBox(moov, "stsz", stsz.Bytes())

		writeBox(buf, "moov", moov.Bytes())

		// mdat with 24 bytes (10 + 14)
		mdat := make([]byte, 24)
		for i := range mdat {
			mdat[i] = byte(i + 1)
		}
		writeBox(buf, "mdat", mdat)

		samples, tel, err := DecodeMP4AAC(buf.Bytes())
		if err != nil {
			t.Fatalf("DecodeMP4AAC failed with stsz: %v", err)
		}
		if tel.DurationMS != 1000 {
			t.Errorf("expected DurationMS=1000, got %d", tel.DurationMS)
		}
		if len(samples) != 2*1024 {
			t.Errorf("expected 2048 samples (2 frames * 1024), got %d", len(samples))
		}
	})
}

func TestOpusFrameEnergyExtraction(t *testing.T) {
	t.Run("CELT silence vs loud frame energy extraction", func(t *testing.T) {
		// CELT FB 20ms frame: config 20 -> TOC = (20 << 3) = 0xA0
		toc := byte(0xA0)
		silentPacket := []byte{toc, 0x00, 0x00, 0x00}
		energies, dur := ExtractOpusFrameEnergy(silentPacket)
		if dur != 20 {
			t.Errorf("expected 20ms duration, got %d", dur)
		}
		for _, e := range energies {
			if e > 0.05 {
				t.Errorf("expected near-zero energy for silent CELT packet, got %f", e)
			}
		}

		loudPacket := []byte{toc, 0xFE, 0xDC, 0xBA, 0x98, 0x76, 0x54, 0x32, 0x10}
		loudEnergies, _ := ExtractOpusFrameEnergy(loudPacket)
		var maxLoud float64
		for _, e := range loudEnergies {
			if e > maxLoud {
				maxLoud = e
			}
		}
		if maxLoud < 0.40 {
			t.Errorf("expected high energy for loud CELT packet, got %f", maxLoud)
		}
	})

	t.Run("SILK subframe gain extraction", func(t *testing.T) {
		// SILK WB 20ms frame: config 9 -> TOC = (9 << 3) = 0x48
		toc := byte(0x48)
		// SILK packet with unvoiced / low gain
		quietSilk := []byte{toc, 0x00, 0x04, 0x04, 0x04, 0x04}
		quietEnergies, dur := ExtractOpusFrameEnergy(quietSilk)
		if dur != 20 {
			t.Errorf("expected 20ms duration, got %d", dur)
		}
		for _, e := range quietEnergies {
			if e > 0.15 {
				t.Errorf("expected low energy for quiet SILK, got %f", e)
			}
		}

		// SILK packet with high subframe gain
		loudSilk := []byte{toc, 0x80, 0x3C, 0x3C, 0x3C, 0x3C}
		loudEnergies, _ := ExtractOpusFrameEnergy(loudSilk)
		var maxLoud float64
		for _, e := range loudEnergies {
			if e > maxLoud {
				maxLoud = e
			}
		}
		if maxLoud < 0.50 {
			t.Errorf("expected high energy for loud SILK, got %f", maxLoud)
		}
	})

	t.Run("DecodeOGGOpus acoustic energy is independent of packet byte padding", func(t *testing.T) {
		// Build OGG Opus with low energy packets but padded with extra bytes
		buf := new(bytes.Buffer)
		buf.WriteString("OggS")
		buf.WriteByte(0)
		buf.WriteByte(0x02) // BOS
		binary.Write(buf, binary.LittleEndian, uint64(0))
		binary.Write(buf, binary.LittleEndian, uint32(999))
		binary.Write(buf, binary.LittleEndian, uint32(0))
		binary.Write(buf, binary.LittleEndian, uint32(0))
		buf.WriteByte(1)
		opusHead := []byte{
			'O', 'p', 'u', 's', 'H', 'e', 'a', 'd',
			1, 1, 0x00, 0x00, 0x80, 0xBB, 0x00, 0x00, 0x00, 0x00, 0,
		}
		buf.WriteByte(byte(len(opusHead)))
		buf.Write(opusHead)

		// Audio Page with 2 frames: one silence (gains = 0), one active speech (gains = high)
		buf.WriteString("OggS")
		buf.WriteByte(0)
		buf.WriteByte(0x04) // EOS
		binary.Write(buf, binary.LittleEndian, uint64(1920)) // 40ms @ 48kHz
		binary.Write(buf, binary.LittleEndian, uint32(999))
		binary.Write(buf, binary.LittleEndian, uint32(1))
		binary.Write(buf, binary.LittleEndian, uint32(0))
		buf.WriteByte(2) // 2 segments

		// Segment 1: silent frame with lots of padding (e.g. 50 bytes of zeros)
		silentPadded := make([]byte, 50)
		silentPadded[0] = 0x48 // SILK 20ms config 9
		buf.WriteByte(50)

		// Segment 2: loud frame with small payload (6 bytes)
		loudSmall := []byte{0x48, 0x80, 0x3E, 0x3E, 0x3E, 0x3E}
		buf.WriteByte(byte(len(loudSmall)))

		buf.Write(silentPadded)
		buf.Write(loudSmall)

		_, tel, err := DecodeOGGOpus(buf.Bytes())
		if err != nil {
			t.Fatalf("DecodeOGGOpus failed: %v", err)
		}
		if tel.DurationMS != 40 {
			t.Errorf("expected 40ms duration, got %d", tel.DurationMS)
		}
		// Since 1 of the 2 20ms frames was silent and 1 was loud, VoicedDurationMS should be 20ms!
		if tel.VoicedDurationMS != 20 {
			t.Errorf("expected VoicedDurationMS = 20, got %d", tel.VoicedDurationMS)
		}
	})
}

func TestAACFrameEnergyExtraction(t *testing.T) {
	t.Run("SCE mono silence vs loud global gain extraction", func(t *testing.T) {
		// SCE: element tag = 0 (3 bits = 000), instance_tag = 0 (4 bits = 0000), global_gain = 0 (8 bits = 00000000)
		// Byte 0: 0x00, Byte 1: 0x00
		silentSCE := []byte{0x00, 0x00, 0x00, 0x00}
		silentRMS, err := ExtractAACFrameEnergy(silentSCE, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if silentRMS > 0.01 {
			t.Errorf("expected silent RMS <= 0.01, got %f", silentRMS)
		}

		// Loud SCE: element tag = 0, instance_tag = 0, global_gain = 180 (0xB4)
		// Bit alignment: 3 bits (0) + 4 bits (0) + 8 bits (180 = 0xB4)
		// Byte 0: (0 << 5) | (0 << 1) | (180 >> 7) = 0x01
		// Byte 1: (180 << 1) = 0x68
		loudSCE := []byte{0x01, 0x68, 0x00, 0x00}
		loudRMS, err := ExtractAACFrameEnergy(loudSCE, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if loudRMS < 0.60 {
			t.Errorf("expected loud RMS >= 0.60, got %f", loudRMS)
		}
	})

	t.Run("CPE stereo global gain extraction", func(t *testing.T) {
		// CPE: element tag = 1 (3 bits = 001), instance_tag = 0 (4 bits = 0000), common_window = 0 (1 bit = 0)
		// ics_1: global_gain = 160 (0xA0), ics_2: global_gain = 160 (0xA0)
		// Byte 0: (1 << 5) | (0 << 1) | 0 = 0x20
		// Byte 1: 160 = 0xA0
		// Byte 2: 160 = 0xA0
		stereoCPE := []byte{0x20, 0xA0, 0xA0, 0x00}
		stereoRMS, err := ExtractAACFrameEnergy(stereoCPE, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if stereoRMS < 0.30 || stereoRMS > 0.70 {
			t.Errorf("expected medium RMS ~0.50, got %f", stereoRMS)
		}
	})

	t.Run("decodeADTSAAC measures acoustic gain rather than byte payload length", func(t *testing.T) {
		buf := new(bytes.Buffer)
		// 2 ADTS frames:
		// Frame 1: silent frame with large byte payload (e.g. 60 bytes)
		// Frame 2: loud frame with small byte payload (e.g. 15 bytes)
		// 44100 Hz (srIdx = 4), Mono (chConfig = 1) -> each frame is 1024 samples = ~23.2ms -> 2 frames = ~46ms

		// Frame 1 header (silent, len = 60)
		h1 := make([]byte, 7)
		h1[0] = 0xFF
		h1[1] = 0xF1
		h1[2] = byte((1 << 6) | ((4 & 0x0F) << 2) | 0)
		h1[3] = byte(((60 >> 11) & 0x03))
		h1[4] = byte((60 >> 3) & 0xFF)
		h1[5] = byte(((60 & 0x07) << 5) | 0x1F)
		h1[6] = 0xFC
		buf.Write(h1)
		silentPayload := make([]byte, 53) // 53 bytes of zeros -> global_gain = 0
		buf.Write(silentPayload)

		// Frame 2 header (loud, len = 15)
		h2 := make([]byte, 7)
		h2[0] = 0xFF
		h2[1] = 0xF1
		h2[2] = byte((1 << 6) | ((4 & 0x0F) << 2) | 0)
		h2[3] = byte(((15 >> 11) & 0x03))
		h2[4] = byte((15 >> 3) & 0xFF)
		h2[5] = byte(((15 & 0x07) << 5) | 0x1F)
		h2[6] = 0xFC
		buf.Write(h2)
		loudPayload := []byte{0x01, 0x68, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00} // global_gain = 180
		buf.Write(loudPayload)

		_, tel, err := DecodeMP4AAC(buf.Bytes())
		if err != nil {
			t.Fatalf("DecodeMP4AAC on ADTS failed: %v", err)
		}

		// Duration of 2 frames @ 44100 is (2048 * 1000) / 44100 = 46ms
		if tel.DurationMS < 40 || tel.DurationMS > 50 {
			t.Errorf("expected DurationMS ~46, got %d", tel.DurationMS)
		}
		// Voiced duration should only count the 1 loud frame (~23ms)
		if tel.VoicedDurationMS < 15 || tel.VoicedDurationMS > 30 {
			t.Errorf("expected VoicedDurationMS ~23, got %d", tel.VoicedDurationMS)
		}
	})
}

func TestEncodeWAV(t *testing.T) {
	t.Run("encodes mono 16kHz PCM into valid RIFF WAV", func(t *testing.T) {
		sine := generateSinePCM16(16000, 440, 1.0, 15000)
		wavData := EncodeWAV(sine, 16000, 1)

		if !IsWAVHeader(wavData) {
			t.Fatalf("expected valid WAV header")
		}

		decodedSamples, tel, err := DecodeWAV(wavData)
		if err != nil {
			t.Fatalf("DecodeWAV on EncodeWAV output failed: %v", err)
		}
		if len(decodedSamples) != len(sine) {
			t.Errorf("expected %d samples, got %d", len(sine), len(decodedSamples))
		}
		if tel.SampleRate != 16000 || tel.Channels != 1 || tel.DurationMS != 1000 {
			t.Errorf("unexpected tel: sr=%d ch=%d dur=%d", tel.SampleRate, tel.Channels, tel.DurationMS)
		}
	})

	t.Run("handles default parameters on zero or negative values", func(t *testing.T) {
		wavData := EncodeWAV([]int16{100, 200, 300}, 0, 0)
		if !IsWAVHeader(wavData) {
			t.Errorf("expected valid WAV header")
		}
	})
}

func TestTranscodeAudio(t *testing.T) {
	t.Run("transcodes Ogg Opus to WAV in-memory", func(t *testing.T) {
		oggBytes := buildOggOpusBytes(1500, 48000)
		wavBytes, tel, err := TranscodeAudio(oggBytes, "audio/wav")
		if err != nil {
			t.Fatalf("TranscodeAudio to WAV failed: %v", err)
		}
		if !IsWAVHeader(wavBytes) {
			t.Errorf("expected WAV header on transcoded output")
		}
		if tel.DurationMS != 1500 {
			t.Errorf("expected DurationMS = 1500, got %d", tel.DurationMS)
		}
	})

	t.Run("transcodes WAV to OGG Opus in-memory", func(t *testing.T) {
		sine := generateSinePCM16(48000, 440, 1.0, 20000)
		wavBytes := EncodeWAV(sine, 48000, 1)

		oggBytes, tel, err := TranscodeAudio(wavBytes, "audio/ogg")
		if err != nil {
			t.Fatalf("TranscodeAudio to OGG failed: %v", err)
		}
		if !IsOGGHeader(oggBytes) {
			t.Errorf("expected OGG header on transcoded output")
		}
		if tel.DurationMS != 1000 {
			t.Errorf("expected DurationMS = 1000, got %d", tel.DurationMS)
		}
	})

	t.Run("transcodes MP3 to ADTS AAC in-memory", func(t *testing.T) {
		mp3Bytes := buildMP3Bytes(20)
		aacBytes, tel, err := TranscodeAudio(mp3Bytes, "audio/aac")
		if err != nil {
			t.Fatalf("TranscodeAudio to AAC failed: %v", err)
		}
		if !IsADTSAACHeader(aacBytes) {
			t.Errorf("expected ADTS AAC header on transcoded output")
		}
		if tel.DurationMS <= 0 {
			t.Errorf("expected positive DurationMS, got %d", tel.DurationMS)
		}
	})

	t.Run("transcodes AAC to MP3 in-memory", func(t *testing.T) {
		aacBytes := buildADTSAACBytes(10, 4, 1)
		mp3Bytes, tel, err := TranscodeAudio(aacBytes, "audio/mpeg")
		if err != nil {
			t.Fatalf("TranscodeAudio to MP3 failed: %v", err)
		}
		if !IsMP3Header(mp3Bytes) {
			t.Errorf("expected MP3 header on transcoded output")
		}
		if tel.DurationMS <= 0 {
			t.Errorf("expected positive DurationMS, got %d", tel.DurationMS)
		}
	})

	t.Run("TranscodeToWAV helper", func(t *testing.T) {
		mp3Bytes := buildMP3Bytes(10)
		wavBytes, tel, err := TranscodeToWAV(mp3Bytes, "audio/mpeg")
		if err != nil {
			t.Fatalf("TranscodeToWAV failed: %v", err)
		}
		if !IsWAVHeader(wavBytes) || tel == nil {
			t.Errorf("expected valid WAV and telemetry")
		}
	})

	t.Run("unsupported target format returns error", func(t *testing.T) {
		sine := generateSinePCM16(16000, 440, 0.5, 10000)
		wavBytes := EncodeWAV(sine, 16000, 1)
		_, _, err := TranscodeAudio(wavBytes, "video/avi")
		if !errors.Is(err, ErrInvalidAudioFormat) {
			t.Errorf("expected ErrInvalidAudioFormat, got %v", err)
		}
	})
}



