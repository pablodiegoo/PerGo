package media

import (
	"bytes"
	"encoding/binary"
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
}
