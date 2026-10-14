package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"unicode/utf8"
)

func riffInfo(data []byte, result *Metadata) error {
	for count := 0; len(data) > 0; count++ {
		if count >= 2048 {
			return ErrMetadataCapacity
		}
		if len(data) < 8 {
			return io.ErrUnexpectedEOF
		}
		kind := string(data[:4])
		size := uint64(binary.LittleEndian.Uint32(data[4:8]))
		data = data[8:]
		padded := size + (size & 1)
		if padded > uint64(len(data)) {
			return io.ErrUnexpectedEOF
		}
		field := bytes.TrimRight(data[:int(size)], "\x00")
		data = data[int(padded):]
		if kind != "INAM" && kind != "IART" && kind != "IPRD" {
			continue
		}
		// Legacy INFO text has no mandatory encoding marker. Preserve UTF-8 where
		// valid, otherwise decode its byte characters instead of inventing a codec.
		var value string
		var err error
		if utf8.Valid(field) {
			value, err = textTag(field, 3)
		} else {
			value, err = textTag(field, 0)
		}
		if err != nil {
			return err
		}
		switch kind {
		case "INAM":
			if result.Title == "" {
				result.Title = value
			}
		case "IART":
			if result.Artist == "" {
				result.Artist = value
			}
		case "IPRD":
			if result.Album == "" {
				result.Album = value
			}
		}
	}
	return nil
}

func (r *tagInput) wav(result *Metadata) error {
	header, err := r.read(0, 12)
	if err != nil {
		return err
	}
	if string(header[8:]) != "WAVE" {
		return errors.New("RIFF is not WAVE")
	}
	end := int64(binary.LittleEndian.Uint32(header[4:8])) + 8
	if end < 12 || end > r.size {
		return errors.New("invalid WAVE bounds")
	}
	for offset, count := int64(12), 0; offset < end; count++ {
		if count >= 2048 {
			return ErrMetadataCapacity
		}
		if end-offset < 8 {
			return io.ErrUnexpectedEOF
		}
		chunk, err := r.read(offset, 8)
		if err != nil {
			return err
		}
		kind := string(chunk[:4])
		size := int64(binary.LittleEndian.Uint32(chunk[4:]))
		offset += 8
		padded := size + (size & 1)
		if padded > end-offset {
			return io.ErrUnexpectedEOF
		}
		if kind == "LIST" {
			if size < 4 {
				return io.ErrUnexpectedEOF
			}
			listType, err := r.read(offset, 4)
			if err != nil {
				return err
			}
			if string(listType) == "INFO" {
				data, err := r.read(offset+4, size-4)
				if err != nil {
					return err
				}
				if err := riffInfo(data, result); err != nil {
					return err
				}
			}
		} else if kind == "id3 " || kind == "ID3 " {
			data, err := r.read(offset, size)
			if err != nil {
				return err
			}
			if len(data) < 10 || string(data[:3]) != "ID3" {
				return errors.New("invalid embedded WAVE ID3 tag")
			}
			tags := &tagInput{ctx: r.ctx, file: bytes.NewReader(data), size: int64(len(data)), remaining: metadataBudget}
			if err := tags.id3(data[:10], result); err != nil {
				return err
			}
		}
		offset += padded
	}
	return nil
}
