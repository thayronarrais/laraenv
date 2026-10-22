package workspacefiles

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

func (s *Service) ReadImage(ref ContextRef, key, relative string) Result {
	data, _, issue := s.readBytes(ref, key, relative, 8<<20)
	if issue.Code != "" {
		return issue
	}
	mime := imageMime(data)
	if mime == "" {
		return problem("unsupported_format", errors.New("not a supported image"))
	}
	return Result{Code: "ok", Image: &Image{DataURL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), Mime: mime, Size: int64(len(data))}}
}

func imageMime(data []byte) string {
	if len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		if validWebP(data) {
			return "image/webp"
		}
		return ""
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	switch format {
	case "png":
		return "image/png"
	case "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	}
	return ""
}

func validWebP(data []byte) bool {
	if len(data) < 20 || uint64(binary.LittleEndian.Uint32(data[4:8])) != uint64(len(data)-8) {
		return false
	}
	var canvasWidth, canvasHeight uint32
	var extended, animated, animationHeader, stillImage bool
	frames := 0
	valid := forWebPChunks(data[12:], func(name string, payload []byte) bool {
		switch name {
		case "VP8X":
			if extended || len(payload) != 10 || payload[0]&0xc1 != 0 || !bytes.Equal(payload[1:4], []byte{0, 0, 0}) {
				return false
			}
			extended = true
			animated = payload[0]&0x02 != 0
			canvasWidth, canvasHeight = webP24(payload[4:7])+1, webP24(payload[7:10])+1
			return uint64(canvasWidth)*uint64(canvasHeight) <= uint64(^uint32(0))
		case "ANIM":
			if !extended || !animated || animationHeader || len(payload) != 6 {
				return false
			}
			animationHeader = true
		case "ANMF":
			if !extended || !animated || !animationHeader || !validWebPFrame(payload, canvasWidth, canvasHeight) {
				return false
			}
			frames++
		case "VP8 ", "VP8L":
			if animated || stillImage {
				return false
			}
			width, height, ok := webPBitstream(name, payload)
			if !ok || (extended && (width != canvasWidth || height != canvasHeight)) {
				return false
			}
			stillImage = true
		}
		return true
	})
	return valid && ((animated && animationHeader && frames > 0 && !stillImage) || (!animated && stillImage && frames == 0))
}

// Each RIFF chunk is bounded by its parent; odd payloads require one zero pad
// byte. This is also used for the nested chunks inside an animation frame.
func forWebPChunks(data []byte, visit func(string, []byte) bool) bool {
	for len(data) > 0 {
		if len(data) < 8 {
			return false
		}
		size := uint64(binary.LittleEndian.Uint32(data[4:8]))
		if size > uint64(len(data)-8) {
			return false
		}
		end := 8 + int(size)
		if !visit(string(data[:4]), data[8:end]) {
			return false
		}
		if size%2 != 0 {
			if end >= len(data) || data[end] != 0 {
				return false
			}
			end++
		}
		data = data[end:]
	}
	return true
}

func webP24(data []byte) uint32 {
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16
}

func validWebPFrame(payload []byte, canvasWidth, canvasHeight uint32) bool {
	// ANMF starts with a 16-byte frame header followed by a bitstream chunk.
	if len(payload) < 24 || payload[15]&0xfc != 0 {
		return false
	}
	x, y := uint64(webP24(payload[0:3]))*2, uint64(webP24(payload[3:6]))*2
	width, height := webP24(payload[6:9])+1, webP24(payload[9:12])+1
	if x+uint64(width) > uint64(canvasWidth) || y+uint64(height) > uint64(canvasHeight) {
		return false
	}
	imageCount := 0
	valid := forWebPChunks(payload[16:], func(name string, data []byte) bool {
		if name == "VP8 " || name == "VP8L" {
			w, h, ok := webPBitstream(name, data)
			if !ok || w != width || h != height {
				return false
			}
			imageCount++
		}
		return true
	})
	return valid && imageCount == 1
}

func webPBitstream(name string, data []byte) (uint32, uint32, bool) {
	switch name {
	case "VP8 ":
		if len(data) < 10 || !bytes.Equal(data[3:6], []byte{0x9d, 0x01, 0x2a}) {
			return 0, 0, false
		}
		w, h := uint32(binary.LittleEndian.Uint16(data[6:8]))&0x3fff, uint32(binary.LittleEndian.Uint16(data[8:10]))&0x3fff
		return w, h, w > 0 && h > 0
	case "VP8L":
		if len(data) < 5 || data[0] != 0x2f {
			return 0, 0, false
		}
		bits := binary.LittleEndian.Uint32(data[1:5])
		return (bits & 0x3fff) + 1, ((bits >> 14) & 0x3fff) + 1, true
	}
	return 0, 0, false
}
