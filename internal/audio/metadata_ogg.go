package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

type oggTags struct {
	sequence  uint32
	codec     string
	packets   int
	continued bool
	ended     bool
	packet    []byte
}

var oggCRCTable = func() (table [256]uint32) {
	for i := range table {
		value := uint32(i) << 24
		for range 8 {
			if value&0x80000000 != 0 {
				value = value<<1 ^ 0x04c11db7
			} else {
				value <<= 1
			}
		}
		table[i] = value
	}
	return
}()

func oggCRC(parts ...[]byte) uint32 {
	var value uint32
	for _, part := range parts {
		for _, b := range part {
			value = value<<8 ^ oggCRCTable[byte(value>>24)^b]
		}
	}
	return value
}

func oggCodec(packet []byte) (string, error) {
	if bytes.HasPrefix(packet, []byte("\x01vorbis")) {
		if len(packet) != 30 || binary.LittleEndian.Uint32(packet[7:11]) != 0 || packet[11] == 0 || binary.LittleEndian.Uint32(packet[12:16]) == 0 || packet[29] != 1 {
			return "", errors.New("invalid Vorbis identification header")
		}
		small, large := packet[28]&15, packet[28]>>4
		if small < 6 || large > 13 || small > large {
			return "", errors.New("invalid Vorbis block sizes")
		}
		return "vorbis", nil
	}
	if bytes.HasPrefix(packet, []byte("OpusHead")) {
		if len(packet) < 19 || packet[8] > 15 || packet[9] == 0 {
			return "", errors.New("invalid Opus identification header")
		}
		if packet[18] == 0 {
			if packet[9] > 2 || len(packet) != 19 {
				return "", errors.New("invalid Opus channel mapping")
			}
		} else if len(packet) < 21+int(packet[9]) {
			return "", io.ErrUnexpectedEOF
		}
		return "opus", nil
	}
	return "ignored", nil
}

func oggComment(packet []byte, codec string, result *Metadata) error {
	if codec == "vorbis" {
		if !bytes.HasPrefix(packet, []byte("\x03vorbis")) {
			return errors.New("missing Vorbis comment header")
		}
		return vorbisComments(packet[7:], result, "vorbis")
	}
	if !bytes.HasPrefix(packet, []byte("OpusTags")) {
		return errors.New("missing Opus comment header")
	}
	return vorbisComments(packet[8:], result, "opus")
}

func (r *tagInput) ogg(result *Metadata) error {
	streams := make(map[uint32]*oggTags)
	retained := 0
	for offset, pages := int64(0), 0; offset < r.size; pages++ {
		if pages >= 2048 {
			return ErrMetadataCapacity
		}
		header, err := r.read(offset, 27)
		if err != nil {
			return err
		}
		if string(header[:4]) != "OggS" || header[4] != 0 || header[5]&^byte(7) != 0 {
			return errors.New("invalid Ogg page header")
		}
		flags := header[5]
		serial := binary.LittleEndian.Uint32(header[14:18])
		sequence := binary.LittleEndian.Uint32(header[18:22])
		laces, err := r.read(offset+27, int64(header[26]))
		if err != nil {
			return err
		}
		size := 0
		for _, lace := range laces {
			size += int(lace)
		}
		body, err := r.read(offset+27+int64(len(laces)), int64(size))
		if err != nil {
			return err
		}
		expected := binary.LittleEndian.Uint32(header[22:26])
		clear(header[22:26])
		if oggCRC(header, laces, body) != expected {
			return errors.New("Ogg page checksum mismatch")
		}
		stream := streams[serial]
		if stream == nil {
			if len(streams) >= 8 {
				return ErrMetadataCapacity
			}
			if flags&2 == 0 || flags&1 != 0 || sequence != 0 {
				return errors.New("Ogg stream lacks initial page")
			}
			stream = &oggTags{}
			streams[serial] = stream
		} else if stream.ended || flags&2 != 0 || sequence != stream.sequence+1 || (flags&1 != 0) != stream.continued {
			return errors.New("broken Ogg page sequence or continuation")
		}
		stream.sequence = sequence
		cursor := 0
		for _, lace := range laces {
			length := int(lace)
			if stream.codec != "ignored" {
				if retained+length > metadataBudget {
					return ErrMetadataCapacity
				}
				stream.packet = append(stream.packet, body[cursor:cursor+length]...)
				retained += length
			}
			cursor += length
			stream.continued = lace == 255
			if stream.continued {
				continue
			}
			if stream.codec != "ignored" {
				if stream.packets == 0 {
					stream.codec, err = oggCodec(stream.packet)
				} else {
					if err := oggComment(stream.packet, stream.codec, result); err != nil {
						return err
					}
					return nil // Do not scan setup packets, audio pages or later chained tracks.
				}
				if err != nil {
					return err
				}
			}
			retained -= len(stream.packet)
			stream.packet = nil
			stream.packets++
		}
		if flags&4 != 0 {
			if stream.continued {
				return errors.New("Ogg ends inside a packet")
			}
			stream.ended = true
		}
		offset += 27 + int64(len(laces)) + int64(size)
	}
	return errors.New("Ogg has no supported audio comment header")
}
