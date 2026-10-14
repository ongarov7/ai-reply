// Package audiotest — тесттерге арналған шағын синтетикалық M4A және WAV файлдары.
//
// Only the container structure is real: the "audio" is a few bytes of
// payload, which is all the duration check and a fake provider ever need.
package audiotest

import (
	"bytes"
	"encoding/binary"
	"time"
)

// U32, U64, U16 — big-endian сандар (MP4); LE32, LE16 — little-endian (WAV).
func U32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func U64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }
func U16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }

func LE32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
func LE16(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }

// Box — бір ISO BMFF қорабы: 32-биттік өлшем, түр, мазмұн.
func Box(kind string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return bytes.Join([][]byte{U32(uint32(8 + len(body))), []byte(kind), body}, nil)
}

// LargeBox — 64-биттік өлшемі бар қорап (size = 1).
func LargeBox(kind string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return bytes.Join([][]byte{U32(1), []byte(kind), U64(uint64(16 + len(body))), body}, nil)
}

// FullBox — нұсқа байты мен үш флаг байты бар қорап.
func FullBox(kind string, version byte, parts ...[]byte) []byte {
	return Box(kind, append([][]byte{{version, 0, 0, 0}}, parts...)...)
}

// FTyp — M4A файлының басы.
func FTyp() []byte { return Box("ftyp", []byte("M4A "), U32(0), []byte("isomM4A mp42")) }

// MVHD — фильм тақырыбы: нұсқа 0 (32-биттік) не 1 (64-биттік) уақыт өрістері.
func MVHD(version byte, timescale uint32, units uint64) []byte {
	return FullBox("mvhd", version, timesFor(version, timescale, units), make([]byte, 80))
}

// Track — trak/mdia: mdhd (timescale, units) және minf/stbl/stts (бір жазба, 1024 үлгіден).
// samples — stts-тағы ұзақтық (units-тен өзгеше болуы мүмкін).
func Track(timescale uint32, units, samples uint64) []byte {
	mdhd := FullBox("mdhd", 0, timesFor(0, timescale, units), make([]byte, 4))
	frames := samples / 1024
	stts := FullBox("stts", 0, U32(1), U32(uint32(frames)), U32(1024))
	return Box("trak", Box("mdia", mdhd, Box("minf", Box("stbl", stts))))
}

func timesFor(version byte, timescale uint32, units uint64) []byte {
	if version == 1 {
		return bytes.Join([][]byte{U64(0), U64(0), U32(timescale), U64(units)}, nil)
	}
	return bytes.Join([][]byte{U32(0), U32(0), U32(timescale), U32(uint32(units))}, nil)
}

// M4A — d ұзақтығы бар файл: ftyp, mdat (payload), содан кейін moov (MediaRecorder реті).
// payload nil болса — бірнеше нөл байт.
func M4A(d time.Duration, payload []byte) []byte {
	if payload == nil {
		payload = make([]byte, 64)
	}
	samples := uint64(d * 44100 / time.Second)
	moov := Box("moov", MVHD(0, 1000, uint64(d/time.Millisecond)), Track(44100, samples, samples))
	return bytes.Join([][]byte{FTyp(), Box("mdat", payload), moov}, nil)
}

// FmtChunk — WAV fmt бөлігі.
func FmtChunk(format, channels uint16, sampleRate, byteRate uint32, blockAlign, bits uint16) []byte {
	return Chunk("fmt ", LE16(format), LE16(channels), LE32(sampleRate), LE32(byteRate), LE16(blockAlign), LE16(bits))
}

// Chunk — бір RIFF бөлігі (тақ өлшем жұпқа толтырылады).
func Chunk(id string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	out := bytes.Join([][]byte{[]byte(id), LE32(uint32(len(body))), body}, nil)
	if len(body)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

// RIFF — WAVE файлы берілген бөліктерден.
func RIFF(chunks ...[]byte) []byte {
	body := bytes.Join(chunks, nil)
	return bytes.Join([][]byte{[]byte("RIFF"), LE32(uint32(4 + len(body))), []byte("WAVE"), body}, nil)
}

// WAV — 16 кГц, моно, 16-биттік PCM үнсіздік, ұзақтығы d.
func WAV(d time.Duration) []byte {
	const rate, align = 16000, 2
	data := make([]byte, int(d*rate/time.Second)*align)
	return RIFF(FmtChunk(1, 1, rate, rate*align, align, 16), Chunk("data", data))
}
