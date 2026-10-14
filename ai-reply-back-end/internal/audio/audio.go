// Package audio — дауыс жазбасының контейнерін тану және ұзақтығын оқу (тек жадта).
//
// The keyboard uploads one recording per request. Its declared content type
// is never trusted: the format is decided by the first bytes, and the length
// is read from the container itself, so the 40-second limit holds whatever
// the client says. Nothing here writes, keeps or logs the audio.
package audio

import (
	"encoding/binary"
	"errors"
	"time"
)

// Қабылданатын контейнерлер.
const (
	// FormatM4A — MP4 контейнеріндегі AAC (Android MediaRecorder, MPEG_4).
	FormatM4A = "m4a"
	// FormatWAV — RIFF/WAVE, PCM.
	FormatWAV = "wav"
)

var (
	// ErrUnsupported — M4A да, WAV та емес (не фрагменттелген MP4, сығылған WAV).
	ErrUnsupported = errors.New("audio: unsupported container")
	// ErrMalformed — контейнер танылды, бірақ ұзақтығын оқу мүмкін емес.
	ErrMalformed = errors.New("audio: malformed container")
)

// maxSeconds — оқылған ұзақтықтың жоғарғы шегі: одан ұзыны бәрібір қабылданбайды,
// ал шек int64 толып кетуден сақтайды.
const maxSeconds = 1 << 20

// Info — контейнерден оқылған мәлімет.
type Info struct {
	Format   string
	Duration time.Duration
}

// Sniff — алғашқы байттар бойынша формат ("" — танылмады).
func Sniff(data []byte) string {
	switch {
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		return FormatM4A
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE":
		return FormatWAV
	}
	return ""
}

// Inspect — формат және контейнердегі нақты ұзақтық.
func Inspect(data []byte) (Info, error) {
	switch format := Sniff(data); format {
	case FormatM4A:
		d, err := m4aDuration(data)
		return Info{Format: format, Duration: d}, err
	case FormatWAV:
		d, err := wavDuration(data)
		return Info{Format: format, Duration: d}, err
	}
	return Info{}, ErrUnsupported
}

// FileName — провайдерге жіберілетін файлдың аты (кеңейтімі форматты айтады).
func FileName(format string) string {
	if format == FormatWAV {
		return "voice.wav"
	}
	return "voice.m4a"
}

// ContentType — провайдерге жіберілетін бөліктің MIME түрі.
func ContentType(format string) string {
	if format == FormatWAV {
		return "audio/wav"
	}
	return "audio/mp4"
}

// ---------------------------------------------------------------- MP4 / M4A

// m4aDuration — moov/mvhd ұзақтығы, бірақ кез келген тректен қысқа емес.
//
// mvhd is what the contract reads, and for a MediaRecorder file it is the
// recording's length. It is also one number a crafted file can understate
// while its sample table carries minutes of audio, so every track's mdhd and
// stts are read too and the longest wins: a genuine recording differs by one
// AAC frame at most. Fragmented MP4 keeps its samples outside moov, where
// this check cannot see them, and is refused.
func m4aDuration(data []byte) (time.Duration, error) {
	var moov []byte
	fragmented := false
	err := eachBox(data, func(kind string, body []byte) {
		switch kind {
		case "moov":
			if moov == nil {
				moov = body
			}
		case "moof":
			fragmented = true
		}
	})
	if err != nil {
		return 0, err
	}
	if fragmented {
		return 0, ErrUnsupported
	}
	if moov == nil {
		return 0, ErrMalformed
	}

	var mvhd []byte
	var tracks [][]byte
	if err := eachBox(moov, func(kind string, body []byte) {
		switch kind {
		case "mvhd":
			if mvhd == nil {
				mvhd = body
			}
		case "trak":
			tracks = append(tracks, body)
		}
	}); err != nil {
		return 0, err
	}
	if mvhd == nil {
		return 0, ErrMalformed
	}
	scale, units, err := timing(mvhd)
	if err != nil {
		return 0, err
	}
	longest := span(units, uint64(scale))
	for _, trak := range tracks {
		d, err := trackDuration(trak)
		if err != nil {
			return 0, err
		}
		longest = max(longest, d)
	}
	return longest, nil
}

// trackDuration — trak/mdia ішіндегі mdhd ұзақтығы мен stts үлгілерінің қосындысының үлкені.
// Бөлігі жоқ трек өткізіліп жіберіледі (0); шектен шығатын қорап — бұзылған файл.
func trackDuration(trak []byte) (time.Duration, error) {
	mdia, err := child(trak, "mdia")
	if mdia == nil || err != nil {
		return 0, err
	}
	mdhd, err := child(mdia, "mdhd")
	if mdhd == nil || err != nil {
		return 0, err
	}
	scale, units, err := timing(mdhd)
	if err != nil {
		return 0, err
	}
	stts, err := path(mdia, "minf", "stbl", "stts")
	if err != nil {
		return 0, err
	}
	if stts != nil {
		samples, err := sampleUnits(stts)
		if err != nil {
			return 0, err
		}
		units = max(units, samples)
	}
	return span(units, uint64(scale)), nil
}

// timing — mvhd/mdhd: нұсқа 0 (32-биттік өрістер) не 1 (64-биттік); timescale және duration.
func timing(body []byte) (uint32, uint64, error) {
	if len(body) < 4 {
		return 0, 0, ErrMalformed
	}
	var scale uint32
	var units uint64
	switch body[0] {
	case 0:
		if len(body) < 20 {
			return 0, 0, ErrMalformed
		}
		scale = binary.BigEndian.Uint32(body[12:16])
		raw := binary.BigEndian.Uint32(body[16:20])
		if raw == ^uint32(0) {
			return 0, 0, ErrMalformed // «белгісіз ұзақтық»
		}
		units = uint64(raw)
	case 1:
		if len(body) < 32 {
			return 0, 0, ErrMalformed
		}
		scale = binary.BigEndian.Uint32(body[20:24])
		units = binary.BigEndian.Uint64(body[24:32])
		if units == ^uint64(0) {
			return 0, 0, ErrMalformed
		}
	default:
		return 0, 0, ErrMalformed
	}
	if scale == 0 {
		return 0, 0, ErrMalformed
	}
	return scale, units, nil
}

// sampleUnits — stts: әр жазбадағы sample_count × sample_delta қосындысы (трек timescale-інде).
func sampleUnits(stts []byte) (uint64, error) {
	if len(stts) < 8 {
		return 0, ErrMalformed
	}
	count := uint64(binary.BigEndian.Uint32(stts[4:8]))
	entries := stts[8:]
	if count > uint64(len(entries))/8 {
		return 0, ErrMalformed
	}
	// Қосынды 2⁶²-ден асса — бәрібір тым ұзын; одан әрі санау толып кетуге әкелер еді.
	const ceiling = uint64(1) << 62
	var total uint64
	for i := uint64(0); i < count; i++ {
		entry := entries[i*8 : i*8+8]
		n := uint64(binary.BigEndian.Uint32(entry[0:4])) * uint64(binary.BigEndian.Uint32(entry[4:8]))
		if n >= ceiling {
			return ceiling, nil
		}
		if total += n; total >= ceiling {
			return total, nil
		}
	}
	return total, nil
}

// path — бірінен соң бірі салынған қораптар (табылмаса nil).
func path(data []byte, kinds ...string) ([]byte, error) {
	for _, kind := range kinds {
		next, err := child(data, kind)
		if next == nil || err != nil {
			return nil, err
		}
		data = next
	}
	return data, nil
}

// child — data ішіндегі осы түрдің алғашқы қорабының мазмұны (табылмаса nil).
func child(data []byte, kind string) ([]byte, error) {
	var found []byte
	err := eachBox(data, func(k string, body []byte) {
		if found == nil && k == kind {
			found = body
		}
	})
	return found, err
}

// eachBox — data ішінде қатар тұрған ISO BMFF қораптарын аралайды.
//
// size 0 means "to the end of the enclosing data" and size 1 a 64-bit size
// after the type. A box that claims more bytes than there are, or fewer than
// its own header, makes the whole file malformed: nothing is read past it.
func eachBox(data []byte, fn func(kind string, body []byte)) error {
	for len(data) > 0 {
		if len(data) < 8 {
			return ErrMalformed
		}
		size := uint64(binary.BigEndian.Uint32(data[0:4]))
		kind := string(data[4:8])
		header := uint64(8)
		switch size {
		case 0:
			size = uint64(len(data))
		case 1:
			if len(data) < 16 {
				return ErrMalformed
			}
			size, header = binary.BigEndian.Uint64(data[8:16]), 16
		}
		if size < header || size > uint64(len(data)) {
			return ErrMalformed
		}
		fn(kind, data[header:size])
		data = data[size:]
	}
	return nil
}

// ---------------------------------------------------------------- WAV

// wavFormats — RIFF/WAVE ішінде қабылданатын кодтау: PCM, IEEE float, A-law,
// µ-law, WAVE_FORMAT_EXTENSIBLE. Блокпен сығылған ADPCM қабылданбайды.
var wavFormats = map[uint16]bool{1: true, 3: true, 6: true, 7: true, 0xFFFE: true}

// wavDuration — data бөлігінің өлшемі / fmt byte rate.
//
// The byte rate must agree with sample rate × block align: a header that
// claims a large byte rate would otherwise make long audio look short. A data
// size left open by a streaming writer (0 or 0xFFFFFFFF), or larger than the
// file, counts the bytes actually present.
func wavDuration(data []byte) (time.Duration, error) {
	var byteRate uint32
	haveFormat := false
	rest := data[12:]
	for len(rest) >= 8 {
		id := string(rest[0:4])
		size := uint64(binary.LittleEndian.Uint32(rest[4:8]))
		rest = rest[8:]
		switch id {
		case "fmt ":
			if size < 16 || size > uint64(len(rest)) {
				return 0, ErrMalformed
			}
			format := binary.LittleEndian.Uint16(rest[0:2])
			channels := binary.LittleEndian.Uint16(rest[2:4])
			sampleRate := binary.LittleEndian.Uint32(rest[4:8])
			byteRate = binary.LittleEndian.Uint32(rest[8:12])
			blockAlign := binary.LittleEndian.Uint16(rest[12:14])
			if !wavFormats[format] {
				return 0, ErrUnsupported
			}
			if channels == 0 || sampleRate == 0 || blockAlign == 0 ||
				uint64(byteRate) != uint64(sampleRate)*uint64(blockAlign) {
				return 0, ErrMalformed
			}
			haveFormat = true
		case "data":
			if !haveFormat {
				return 0, ErrMalformed
			}
			if size == 0 || size == uint64(^uint32(0)) || size > uint64(len(rest)) {
				size = uint64(len(rest))
			}
			return span(size, uint64(byteRate)), nil
		}
		// Бөліктер жұп байтқа тураланады.
		skip := size + size&1
		if skip > uint64(len(rest)) {
			return 0, ErrMalformed
		}
		rest = rest[skip:]
	}
	return 0, ErrMalformed
}

// span — units / scale секунд, наносекунд дәлдігімен; maxSeconds-тан асса — maxSeconds.
func span(units, scale uint64) time.Duration {
	if scale == 0 {
		return 0
	}
	seconds := units / scale
	if seconds >= maxSeconds {
		return maxSeconds * time.Second
	}
	// scale ≤ 2³² және time.Second < 2³⁰, сондықтан көбейтінді uint64-ке сыяды.
	return time.Duration(seconds)*time.Second + time.Duration(units%scale*uint64(time.Second)/scale)
}
