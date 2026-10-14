package audio

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

func sizedField(data *[]byte, order binary.ByteOrder) ([]byte, error) {
	if len(*data) < 4 {
		return nil, io.ErrUnexpectedEOF
	}
	size := uint64(order.Uint32((*data)[:4]))
	*data = (*data)[4:]
	if size > uint64(len(*data)) {
		return nil, io.ErrUnexpectedEOF
	}
	field := (*data)[:int(size)]
	*data = (*data)[int(size):]
	return field, nil
}

func vorbisComments(data []byte, result *Metadata, framing bool) error {
	vendor, err := sizedField(&data, binary.LittleEndian)
	if err != nil {
		return err
	}
	if !utf8.Valid(vendor) {
		return errors.New("invalid comment vendor UTF-8")
	}
	if len(data) < 4 {
		return io.ErrUnexpectedEOF
	}
	count := binary.LittleEndian.Uint32(data[:4])
	data = data[4:]
	if count > 2048 {
		return ErrMetadataCapacity
	}
	for range count {
		field, err := sizedField(&data, binary.LittleEndian)
		if err != nil {
			return err
		}
		if !utf8.Valid(field) {
			return errors.New("invalid audio comment UTF-8")
		}
		key, value, found := strings.Cut(string(field), "=")
		if !found || key == "" {
			return errors.New("invalid audio comment field")
		}
		for _, b := range []byte(key) {
			if b < 32 || b > 126 || b == '=' {
				return errors.New("invalid audio comment key")
			}
		}
		switch strings.ToUpper(key) {
		case "TITLE", "ARTIST", "ALBUM":
			if len(value) > 4096 {
				return ErrMetadataCapacity
			}
			value = strings.TrimSpace(value)
			switch strings.ToUpper(key) {
			case "TITLE":
				if result.Title == "" {
					result.Title = value
				}
			case "ARTIST":
				if result.Artist == "" {
					result.Artist = value
				}
			case "ALBUM":
				if result.Album == "" {
					result.Album = value
				}
			}
		case "METADATA_BLOCK_PICTURE":
			picture, err := base64.StdEncoding.DecodeString(value)
			if err != nil {
				return errors.New("invalid comment picture encoding")
			}
			if err := flacPicture(picture, result); err != nil {
				return err
			}
		}
	}
	// Vorbis adds a framing byte, whereas FLAC and Opus do not.
	if !framing && len(data) != 0 || framing && (len(data) != 1 || data[0] != 1) {
		return errors.New("unexpected audio comment trailing bytes")
	}
	return nil
}

func flacPicture(data []byte, result *Metadata) error {
	if len(data) < 4 {
		return io.ErrUnexpectedEOF
	}
	kind := binary.BigEndian.Uint32(data[:4])
	data = data[4:]
	mime, err := sizedField(&data, binary.BigEndian)
	if err != nil {
		return err
	}
	description, err := sizedField(&data, binary.BigEndian)
	if err != nil {
		return err
	}
	if !utf8.Valid(description) {
		return errors.New("invalid picture description UTF-8")
	}
	if len(data) < 16 {
		return io.ErrUnexpectedEOF
	}
	data = data[16:] // dimensions/depth/palette
	picture, err := sizedField(&data, binary.BigEndian)
	if err != nil {
		return err
	}
	if len(data) != 0 {
		return errors.New("unexpected picture trailing bytes")
	}
	if string(mime) == "-->" {
		return nil
	} // Never fetch external picture URLs.
	image, err := embeddedPicture(picture)
	if err != nil {
		return err
	}
	if len(result.Picture) == 0 || kind == 3 && !result.pictureFront {
		result.Picture = image
		result.pictureFront = kind == 3
	}
	return nil
}

func (r *tagInput) flac(result *Metadata) error {
	offset := int64(4)
	comments := false
	for block := 0; block < 2048; block++ {
		header, err := r.read(offset, 4)
		if err != nil {
			return err
		}
		offset += 4
		kind := header[0] & 127
		size := int64(header[1])<<16 | int64(header[2])<<8 | int64(header[3])
		if size > r.size-offset {
			return io.ErrUnexpectedEOF
		}
		if block == 0 && (kind != 0 || size != 34) {
			return errors.New("FLAC lacks initial streaminfo")
		}
		if kind == 127 {
			return errors.New("invalid FLAC metadata type")
		}
		if kind == 4 || kind == 6 {
			data, err := r.read(offset, size)
			if err != nil {
				return err
			}
			if kind == 4 {
				if comments {
					return errors.New("duplicate FLAC comment block")
				}
				comments = true
				err = vorbisComments(data, result, false)
			} else {
				err = flacPicture(data, result)
			}
			if err != nil {
				return err
			}
		}
		offset += size
		if header[0]&128 != 0 {
			return nil
		}
	}
	return ErrMetadataCapacity
}
