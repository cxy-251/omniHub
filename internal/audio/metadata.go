package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Metadata contains optional labels and one embedded picture. Missing tags are
// empty; malformed or over-budget metadata returns an error, never partial data.
type Metadata struct {
	Title        string `json:"title"`
	Artist       string `json:"artist"`
	Album        string `json:"album"`
	HasCover     bool   `json:"has_cover"`
	Picture      []byte `json:"-"`
	pictureFront bool
}

const metadataBudget = 8 << 20

// ErrMetadataCapacity identifies excessive tag bytes, boxes or nesting.
var ErrMetadataCapacity = errors.New("audio metadata exceeds capacity")

type tagInput struct {
	ctx       context.Context
	file      io.ReaderAt
	size      int64
	remaining int64
	boxes     int
}

func (r *tagInput) read(offset, size int64) ([]byte, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if offset < 0 || size < 0 || offset > r.size || size > r.size-offset {
		return nil, io.ErrUnexpectedEOF
	}
	if size > r.remaining {
		return nil, ErrMetadataCapacity
	}
	r.remaining -= size
	data := make([]byte, int(size))
	_, err := r.file.ReadAt(data, offset)
	return data, err
}

// ReadMetadata reads ID3/MP4/FLAC/WAVE labels and a bounded embedded picture from
// an already opened regular file. MP4 media payload is skipped by offset, so a
// trailing moov does not require scanning the audio. Total tag reads cap at 8MiB.
func ReadMetadata(ctx context.Context, file *os.File) (Metadata, error) {
	info, err := file.Stat()
	if err != nil {
		return Metadata{}, err
	}
	if !info.Mode().IsRegular() {
		return Metadata{}, errors.New("audio must be a regular file")
	}
	r := &tagInput{ctx: ctx, file: file, size: info.Size(), remaining: metadataBudget}
	result := Metadata{}
	if r.size < 10 {
		return result, ctx.Err()
	}
	prefix, err := r.read(0, 10)
	if err != nil {
		return result, err
	}
	if string(prefix[:3]) == "ID3" {
		err = r.id3(prefix, &result)
	} else if string(prefix[4:8]) == "ftyp" {
		err = r.mp4(0, r.size, 0, "", &result)
	} else if string(prefix[:4]) == "fLaC" {
		err = r.flac(&result)
	} else if string(prefix[:4]) == "RIFF" {
		err = r.wav(&result)
	}
	if err != nil {
		return Metadata{}, err
	}
	if r.size >= 128 && (result.Title == "" || result.Artist == "" || result.Album == "") {
		tail, err := r.read(r.size-128, 128)
		if err != nil {
			return Metadata{}, err
		}
		if string(tail[:3]) == "TAG" {
			if result.Title == "" {
				result.Title = latinText(tail[3:33])
			}
			if result.Artist == "" {
				result.Artist = latinText(tail[33:63])
			}
			if result.Album == "" {
				result.Album = latinText(tail[63:93])
			}
		}
	}
	result.HasCover = len(result.Picture) > 0
	return result, ctx.Err()
}

func latinText(data []byte) string {
	runes := make([]rune, 0, len(data))
	for _, b := range data {
		if b == 0 {
			break
		}
		runes = append(runes, rune(b))
	}
	return strings.TrimSpace(string(runes))
}

func textTag(data []byte, encoding byte) (string, error) {
	var value string
	switch encoding {
	case 0:
		value = latinText(data)
	case 3:
		if !utf8.Valid(data) {
			return "", errors.New("invalid UTF-8 audio label")
		}
		value = string(data)
	case 1, 2:
		var order binary.ByteOrder = binary.BigEndian
		if encoding == 1 {
			if len(data) < 2 {
				return "", io.ErrUnexpectedEOF
			}
			if data[0] == 255 && data[1] == 254 {
				order = binary.LittleEndian
			} else if data[0] != 254 || data[1] != 255 {
				return "", errors.New("UTF-16 label lacks BOM")
			}
			data = data[2:]
		}
		if len(data)%2 != 0 {
			return "", errors.New("odd UTF-16 audio label")
		}
		words := make([]uint16, len(data)/2)
		for i := range words {
			words[i] = order.Uint16(data[i*2:])
		}
		value = string(utf16.Decode(words))
	default:
		return "", errors.New("unsupported audio text encoding")
	}
	value = strings.TrimSpace(strings.SplitN(value, "\x00", 2)[0])
	if len(value) > 4096 {
		return "", ErrMetadataCapacity
	}
	return value, nil
}

func synchsafe(data []byte) (int, error) {
	value := 0
	for _, b := range data {
		if b&128 != 0 {
			return 0, errors.New("invalid ID3 size")
		}
		value = value<<7 | int(b)
	}
	return value, nil
}

func unsynchronise(data []byte) []byte {
	out := data[:0]
	for i := 0; i < len(data); i++ {
		out = append(out, data[i])
		if data[i] == 255 && i+1 < len(data) && data[i+1] == 0 {
			i++
		}
	}
	return out
}

func (r *tagInput) id3(header []byte, result *Metadata) error {
	version := header[3]
	if version < 2 || version > 4 {
		return errors.New("unsupported ID3 version")
	}
	size, err := synchsafe(header[6:10])
	if err != nil {
		return err
	}
	data, err := r.read(10, int64(size))
	if err != nil {
		return err
	}
	if version == 2 && header[5]&64 != 0 {
		return errors.New("compressed ID3 tag unsupported")
	}
	if version < 4 && header[5]&128 != 0 {
		data = unsynchronise(data)
	}
	if version >= 3 && header[5]&64 != 0 {
		if len(data) < 4 {
			return io.ErrUnexpectedEOF
		}
		skip := int(binary.BigEndian.Uint32(data[:4])) + 4
		if version == 4 {
			skip, err = synchsafe(data[:4])
			if err != nil {
				return err
			}
		}
		if skip < 4 || skip > len(data) {
			return errors.New("invalid extended ID3 header")
		}
		data = data[skip:]
	}
	frames := 0
	for len(data) > 0 {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if data[0] == 0 {
			break
		}
		frames++
		if frames > 2048 {
			return ErrMetadataCapacity
		}
		width := 10
		idWidth := 4
		if version == 2 {
			width = 6
			idWidth = 3
		}
		if len(data) < width {
			return io.ErrUnexpectedEOF
		}
		id := string(data[:idWidth])
		length := 0
		if version == 2 {
			length = int(data[3])<<16 | int(data[4])<<8 | int(data[5])
		} else {
			length = int(binary.BigEndian.Uint32(data[4:8]))
		}
		if version == 4 {
			length, err = synchsafe(data[4:8])
			if err != nil {
				return err
			}
		}
		if length > len(data)-width {
			return io.ErrUnexpectedEOF
		}
		frame := data[width : width+length]
		flags := byte(0)
		if version >= 3 {
			flags = data[9]
		}
		data = data[width+length:]
		if id != "TIT2" && id != "TT2" && id != "TPE1" && id != "TP1" && id != "TALB" && id != "TAL" && id != "APIC" && id != "PIC" {
			continue
		}
		if version == 3 && flags&0xc0 != 0 || version == 4 && flags&0x0c != 0 {
			return errors.New("compressed/encrypted audio label unsupported")
		}
		if version == 3 && flags&32 != 0 || version == 4 && flags&64 != 0 {
			if len(frame) < 1 {
				return io.ErrUnexpectedEOF
			}
			frame = frame[1:]
		}
		if version == 4 && flags&1 != 0 {
			if len(frame) < 4 {
				return io.ErrUnexpectedEOF
			}
			frame = frame[4:]
		}
		if version == 4 && (flags&2 != 0 || header[5]&128 != 0) {
			frame = unsynchronise(frame)
		}
		if len(frame) < 1 {
			continue
		}
		if id == "APIC" || id == "PIC" {
			picture, err := id3Picture(frame, version)
			if err != nil {
				return err
			}
			if len(result.Picture) == 0 {
				result.Picture = picture
			}
			continue
		}
		value, err := textTag(frame[1:], frame[0])
		if err != nil {
			return err
		}
		switch id {
		case "TIT2", "TT2":
			result.Title = value
		case "TPE1", "TP1":
			result.Artist = value
		case "TALB", "TAL":
			result.Album = value
		}
	}
	return nil
}

func id3Picture(data []byte, version byte) ([]byte, error) {
	encoding := data[0]
	data = data[1:]
	if version == 2 {
		if len(data) < 4 {
			return nil, io.ErrUnexpectedEOF
		}
		data = data[3:]
	} else {
		end := bytes.IndexByte(data, 0)
		if end < 0 {
			return nil, io.ErrUnexpectedEOF
		}
		data = data[end+1:]
	}
	if len(data) < 1 {
		return nil, io.ErrUnexpectedEOF
	}
	data = data[1:] // picture type
	step := 1
	if encoding == 1 || encoding == 2 {
		step = 2
	}
	found := false
	for i := 0; i+step <= len(data); i += step {
		if data[i] == 0 && (step == 1 || data[i+1] == 0) {
			data = data[i+step:]
			found = true
			break
		}
	}
	if !found {
		return nil, io.ErrUnexpectedEOF
	}
	return embeddedPicture(data)
}

func embeddedPicture(data []byte) ([]byte, error) {
	mime := http.DetectContentType(data)
	if mime != "image/jpeg" && mime != "image/png" && mime != "image/gif" && mime != "image/webp" {
		return nil, errors.New("unsupported embedded cover")
	}
	return append([]byte(nil), data...), nil
}

func (r *tagInput) mp4(start, end int64, depth int, item string, result *Metadata) error {
	if depth > 8 {
		return ErrMetadataCapacity
	}
	for start < end {
		r.boxes++
		if r.boxes > 2048 {
			return ErrMetadataCapacity
		}
		if end-start < 8 {
			return io.ErrUnexpectedEOF
		}
		header, err := r.read(start, 8)
		if err != nil {
			return err
		}
		size := int64(binary.BigEndian.Uint32(header[:4]))
		kind := string(header[4:])
		width := int64(8)
		if size == 1 {
			large, err := r.read(start+8, 8)
			if err != nil {
				return err
			}
			value := binary.BigEndian.Uint64(large)
			if value > uint64(end-start) {
				return io.ErrUnexpectedEOF
			}
			size = int64(value)
			width = 16
		}
		if size == 0 {
			size = end - start
		}
		if size < width || size > end-start {
			return errors.New("invalid MP4 box bounds")
		}
		body := start + width
		next := start + size
		switch kind {
		case "moov", "udta", "ilst":
			err = r.mp4(body, next, depth+1, "", result)
		case "meta":
			if next-body < 4 {
				return io.ErrUnexpectedEOF
			}
			err = r.mp4(body+4, next, depth+1, "", result)
		case "\xa9nam", "\xa9ART", "aART", "\xa9alb", "covr":
			err = r.mp4(body, next, depth+1, kind, result)
		case "data":
			if item != "" {
				data, e := r.read(body, next-body)
				if e != nil {
					return e
				}
				if len(data) < 8 {
					return io.ErrUnexpectedEOF
				}
				if item == "covr" {
					mime := http.DetectContentType(data[8:])
					if mime != "image/jpeg" && mime != "image/png" {
						return errors.New("unsupported MP4 cover")
					}
					if len(result.Picture) == 0 {
						result.Picture = append([]byte(nil), data[8:]...)
					}
				} else {
					encoding := byte(3)
					dataType := binary.BigEndian.Uint32(data[:4]) & 0xffffff
					if dataType != 1 && dataType != 2 {
						return errors.New("unsupported MP4 label type")
					}
					if dataType == 2 {
						encoding = 2
					}
					value, e := textTag(data[8:], encoding)
					if e != nil {
						return e
					}
					switch item {
					case "\xa9nam":
						result.Title = value
					case "\xa9ART", "aART":
						if result.Artist == "" {
							result.Artist = value
						}
					case "\xa9alb":
						result.Album = value
					}
				}
			}
		}
		if err != nil {
			return err
		}
		start = next
	}
	return nil
}
