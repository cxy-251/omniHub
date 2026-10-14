package audio

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func oggPage(serial, sequence uint32, flags byte, laces, body []byte) []byte {
	header := make([]byte, 27)
	copy(header, "OggS")
	header[5] = flags
	header[26] = byte(len(laces))
	binary.LittleEndian.PutUint32(header[14:], serial)
	binary.LittleEndian.PutUint32(header[18:], sequence)
	binary.LittleEndian.PutUint32(header[22:], oggCRC(header, laces, body))
	out := append(header, laces...)
	return append(out, body...)
}
func oggLaces(size int) []byte {
	out := make([]byte, 0, size/255+1)
	for size >= 255 {
		out = append(out, 255)
		size -= 255
	}
	return append(out, byte(size))
}
func vorbisIdentification() []byte {
	packet := make([]byte, 30)
	copy(packet, "\x01vorbis")
	packet[11] = 2
	binary.LittleEndian.PutUint32(packet[12:], 44100)
	packet[28] = 0xb8
	packet[29] = 1
	return packet
}

func TestOggVorbisCrossPageCommentsAndPicture(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	sized := func(value []byte) []byte {
		length := make([]byte, 4)
		binary.BigEndian.PutUint32(length, uint32(len(value)))
		return append(length, value...)
	}
	picture := append([]byte{0, 0, 0, 3}, sized([]byte("image/png"))...)
	picture = append(picture, sized(nil)...)
	picture = append(picture, make([]byte, 16)...)
	picture = append(picture, sized(imageData.Bytes())...)
	comments := append([]byte("\x03vorbis"), commentFields("TITLE=跨页标题", "ARTIST=作者", "DESCRIPTION="+strings.Repeat("x", 500), "METADATA_BLOCK_PICTURE="+base64.StdEncoding.EncodeToString(picture))...)
	comments = append(comments, 1)
	initial := vorbisIdentification()
	data := oggPage(12, 0, 2, oggLaces(len(initial)), initial)
	data = append(data, oggPage(12, 1, 0, []byte{255}, comments[:255])...)
	data = append(data, oggPage(12, 2, 1, oggLaces(len(comments)-255), comments[255:])...)
	// Broken audio pages after metadata must not be scanned or decoded.
	data = append(data, []byte("not an Ogg audio page")...)
	result, err := readContainer(t, data)
	if err != nil || result.Title != "跨页标题" || result.Artist != "作者" || !bytes.Equal(result.Picture, imageData.Bytes()) {
		t.Fatalf("cross-page comment: %+v %v", result, err)
	}
}

func TestOggOpusTagsPaddingAndMultiplexedStream(t *testing.T) {
	ignored := []byte("fishead\x00")
	data := oggPage(99, 0, 2, oggLaces(len(ignored)), ignored)
	identification := append([]byte("OpusHead"), make([]byte, 11)...)
	identification[8] = 1
	identification[9] = 2
	data = append(data, oggPage(12, 0, 2, oggLaces(len(identification)), identification)...)
	tags := append([]byte("OpusTags"), commentFields("TITLE=Opus", "ALBUM=专辑")...)
	tags = append(tags, make([]byte, 100)...)
	data = append(data, oggPage(12, 1, 0, oggLaces(len(tags)), tags)...)
	result, err := readContainer(t, data)
	if err != nil || result.Title != "Opus" || result.Album != "专辑" {
		t.Fatalf("Opus tags: %+v %v", result, err)
	}
}

func TestOggRejectsChecksumSequenceAndContinuationErrors(t *testing.T) {
	identification := vorbisIdentification()
	initial := oggPage(12, 0, 2, oggLaces(len(identification)), identification)
	comments := append([]byte("\x03vorbis"), commentFields("TITLE=test", "DESCRIPTION="+strings.Repeat("x", 500))...)
	comments = append(comments, 1)
	corrupted := append([]byte(nil), initial...)
	corrupted[len(corrupted)-1] ^= 1
	for _, data := range [][]byte{
		corrupted,
		append(append([]byte(nil), initial...), oggPage(12, 3, 0, oggLaces(len(comments)), comments)...),
		append(append(append([]byte(nil), initial...), oggPage(12, 1, 0, []byte{255}, comments[:255])...), oggPage(12, 2, 0, oggLaces(len(comments)-255), comments[255:])...),
		append(append([]byte(nil), initial...), oggPage(12, 1, 0, []byte{255}, comments[:255])...),
	} {
		if _, err := readContainer(t, data); err == nil {
			t.Fatal("broken Ogg metadata accepted")
		}
	}
}

func TestOggHeaderReadBudgetAndCancellation(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "large.ogg"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	id := vorbisIdentification()
	data := oggPage(12, 0, 2, oggLaces(len(id)), id)
	for seq := uint32(1); seq < 140; seq++ {
		flags := byte(1)
		if seq == 1 {
			flags = 0
		}
		laces := bytes.Repeat([]byte{255}, 255)
		data = append(data, oggPage(12, seq, flags, laces, make([]byte, 255*255))...)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMetadata(context.Background(), f); !errors.Is(err, ErrMetadataCapacity) {
		t.Fatalf("oversized comment: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadMetadata(ctx, f); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Ogg read: %v", err)
	}
}
