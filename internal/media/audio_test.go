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
		1,                      // Version
		1,                      // Channels = 1
		0x00, 0x00,             // Pre-skip = 0
		0x80, 0xBB, 0x00, 0x00, // Input sample rate = 48000
		0x00, 0x00,             // Output gain = 0
		0,                      // Mapping family = 0
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
