package audio_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/audio"
	"github.com/aireply/ai-reply-back-end/internal/audio/audiotest"
)

func inspect(t *testing.T, data []byte) audio.Info {
	t.Helper()
	info, err := audio.Inspect(data)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	return info
}

// MediaRecorder реті: ftyp, mdat, moov соңында; mvhd нұсқа 0 және 1.
func TestM4ADurationFromTheMovieHeader(t *testing.T) {
	if info := inspect(t, audiotest.M4A(12500*time.Millisecond, nil)); info.Format != audio.FormatM4A ||
		info.Duration < 12500*time.Millisecond || info.Duration > 12510*time.Millisecond {
		t.Fatalf("version 0: %+v", info)
	}

	// Нұсқа 1, moov mdat-тан бұрын, mdat 64-биттік өлшеммен.
	v1 := bytes.Join([][]byte{audiotest.FTyp(), audiotest.Box("moov", audiotest.MVHD(1, 600, 600*3/2)), audiotest.LargeBox("mdat", make([]byte, 32))}, nil)
	if info := inspect(t, v1); info.Duration != 1500*time.Millisecond {
		t.Fatalf("version 1: %+v", info)
	}

	// size 0 — «файлдың соңына дейін».
	open := bytes.Join([][]byte{audiotest.FTyp(), audiotest.Box("moov", audiotest.MVHD(0, 1000, 2000)), audiotest.U32(0), []byte("mdat"), make([]byte, 16)}, nil)
	if info := inspect(t, open); info.Duration != 2*time.Second {
		t.Fatalf("open-ended mdat: %+v", info)
	}
}

// mvhd қысқа деп айтса да, трек (mdhd, stts) ұзын болса — ұзыны есептеледі.
func TestM4ATheLongestTrackWins(t *testing.T) {
	understated := bytes.Join([][]byte{audiotest.FTyp(), audiotest.Box("mdat", make([]byte, 8)),
		audiotest.Box("moov", audiotest.MVHD(0, 1000, 5000), audiotest.Track(44100, 44100*5, 44100*300))}, nil)
	if info := inspect(t, understated); info.Duration < 299*time.Second {
		t.Fatalf("stts must win: %v", info.Duration)
	}
	header := bytes.Join([][]byte{audiotest.FTyp(), audiotest.Box("moov", audiotest.MVHD(0, 1000, 5000), audiotest.Track(8000, 8000*90, 0))}, nil)
	if info := inspect(t, header); info.Duration != 90*time.Second {
		t.Fatalf("mdhd must win: %v", info.Duration)
	}
}

func TestM4ARefusesWhatItCannotMeasure(t *testing.T) {
	for name, data := range map[string][]byte{
		"no moov":            bytes.Join([][]byte{audiotest.FTyp(), audiotest.Box("mdat", make([]byte, 64))}, nil),
		"no mvhd":            bytes.Join([][]byte{audiotest.FTyp(), audiotest.Box("moov", audiotest.Box("udta"))}, nil),
		"zero timescale":     bytes.Join([][]byte{audiotest.FTyp(), audiotest.Box("moov", audiotest.MVHD(0, 0, 1000))}, nil),
		"unknown duration":   bytes.Join([][]byte{audiotest.FTyp(), audiotest.Box("moov", audiotest.MVHD(0, 1000, 0xFFFFFFFF))}, nil),
		"unknown version":    bytes.Join([][]byte{audiotest.FTyp(), audiotest.Box("moov", audiotest.MVHD(2, 1000, 1000))}, nil),
		"box past the end":   bytes.Join([][]byte{audiotest.FTyp(), audiotest.U32(4096), []byte("free")}, nil),
		"box below a header": bytes.Join([][]byte{audiotest.FTyp(), audiotest.U32(4), []byte("free")}, nil),
		"truncated mvhd":     bytes.Join([][]byte{audiotest.FTyp(), audiotest.Box("moov", audiotest.Box("mvhd", []byte{0, 0, 0, 0, 1}))}, nil),
		"stts past its box": bytes.Join([][]byte{audiotest.FTyp(), audiotest.Box("moov", audiotest.MVHD(0, 1000, 1000),
			audiotest.Box("trak", audiotest.Box("mdia", audiotest.FullBox("mdhd", 0, audiotest.U32(0), audiotest.U32(0), audiotest.U32(1000), audiotest.U32(1000), audiotest.U32(0)),
				audiotest.Box("minf", audiotest.Box("stbl", audiotest.FullBox("stts", 0, audiotest.U32(1000), audiotest.U32(1), audiotest.U32(1)))))))}, nil),
	} {
		if _, err := audio.Inspect(data); !errors.Is(err, audio.ErrMalformed) {
			t.Errorf("%s: %v, want ErrMalformed", name, err)
		}
	}
	fragmented := bytes.Join([][]byte{audiotest.FTyp(), audiotest.Box("moov", audiotest.MVHD(0, 1000, 0)), audiotest.Box("moof"), audiotest.Box("mdat")}, nil)
	if _, err := audio.Inspect(fragmented); !errors.Is(err, audio.ErrUnsupported) {
		t.Fatalf("fragmented MP4: %v", err)
	}
}

func TestWAVDurationFromByteRateAndDataSize(t *testing.T) {
	if info := inspect(t, audiotest.WAV(3*time.Second)); info.Format != audio.FormatWAV || info.Duration != 3*time.Second {
		t.Fatalf("pcm: %+v", info)
	}

	// Тақ өлшемді LIST бөлігі data-дан бұрын; 44.1 кГц стерео.
	stereo := audiotest.RIFF(audiotest.FmtChunk(1, 2, 44100, 44100*4, 4, 16), audiotest.Chunk("LIST", []byte("INFOx")),
		audiotest.Chunk("data", make([]byte, 44100*4/2)))
	if info := inspect(t, stereo); info.Duration != 500*time.Millisecond {
		t.Fatalf("stereo with LIST: %+v", info)
	}

	// Ағынмен жазылған файл: data өлшемі ашық қалған — бар байттар есептеледі.
	streamed := audiotest.RIFF(audiotest.FmtChunk(1, 1, 8000, 16000, 2, 16), []byte("data"), audiotest.LE32(0xFFFFFFFF), make([]byte, 16000))
	if info := inspect(t, streamed); info.Duration != time.Second {
		t.Fatalf("streamed: %+v", info)
	}
}

func TestWAVRefusesLiesAndCompression(t *testing.T) {
	for name, data := range map[string][]byte{
		// byte rate тым үлкен: ұзын жазба қысқа болып көрінер еді.
		"inflated byte rate": audiotest.RIFF(audiotest.FmtChunk(1, 1, 16000, 1_000_000, 2, 16), audiotest.Chunk("data", make([]byte, 64))),
		"no fmt":             audiotest.RIFF(audiotest.Chunk("data", make([]byte, 64))),
		"no data":            audiotest.RIFF(audiotest.FmtChunk(1, 1, 16000, 32000, 2, 16)),
		"zero sample rate":   audiotest.RIFF(audiotest.FmtChunk(1, 1, 0, 0, 2, 16), audiotest.Chunk("data", make([]byte, 64))),
		"chunk past the end": audiotest.RIFF(audiotest.FmtChunk(1, 1, 16000, 32000, 2, 16), []byte("LIST"), audiotest.LE32(4096)),
	} {
		if _, err := audio.Inspect(data); !errors.Is(err, audio.ErrMalformed) {
			t.Errorf("%s: %v, want ErrMalformed", name, err)
		}
	}
	adpcm := audiotest.RIFF(audiotest.FmtChunk(0x11, 1, 16000, 8110, 256, 4), audiotest.Chunk("data", make([]byte, 64)))
	if _, err := audio.Inspect(adpcm); !errors.Is(err, audio.ErrUnsupported) {
		t.Fatalf("ADPCM: %v", err)
	}
}

// Формат мазмұннан анықталады: Ogg, MP3, бос дене — қабылданбайды.
func TestSniffDecidesByContent(t *testing.T) {
	for name, data := range map[string][]byte{
		"ogg":   append([]byte("OggS"), make([]byte, 60)...),
		"mp3":   append([]byte("ID3\x03"), make([]byte, 60)...),
		"riff":  append([]byte("RIFF\x00\x00\x00\x00AVI "), make([]byte, 60)...),
		"short": []byte("ftyp"),
		"empty": nil,
	} {
		if _, err := audio.Inspect(data); !errors.Is(err, audio.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	if audio.FileName(audio.FormatWAV) != "voice.wav" || audio.ContentType(audio.FormatWAV) != "audio/wav" ||
		audio.FileName(audio.FormatM4A) != "voice.m4a" || audio.ContentType(audio.FormatM4A) != "audio/mp4" {
		t.Fatal("file names and content types")
	}
}
