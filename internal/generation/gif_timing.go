package generation

import (
	"encoding/binary"
	"errors"
)

// retimeGIF only changes graphic-control delays. Image descriptors, palettes,
// compressed pixels, disposal, transparency and loop extensions stay intact.
func retimeGIF(data []byte, durationMS int) ([]byte, error) {
	if durationMS < 20 || durationMS > 2000 {
		return nil, errors.New("GIF 每帧时长应为 20–2000 毫秒。")
	}
	if err := validateGIF(data); err != nil {
		return nil, err
	}
	delay := uint16((durationMS + 5) / 10)
	pos := 13
	if data[10]&0x80 != 0 {
		pos += 3 * (1 << ((data[10] & 7) + 1))
	}
	output := append([]byte(nil), data[:pos]...)
	pendingControl := false
	skipBlocks := func() {
		for {
			count := int(data[pos])
			pos++
			if count == 0 {
				return
			}
			pos += count
		}
	}
	for pos < len(data) {
		start := pos
		marker := data[pos]
		pos++
		switch marker {
		case 0x21:
			label := data[pos]
			pos++
			sizePos := pos
			skipBlocks()
			base := len(output)
			output = append(output, data[start:pos]...)
			if label == 0xf9 {
				if data[sizePos] != 4 {
					return nil, errors.New("GIF 播放控制数据无效。")
				}
				binary.LittleEndian.PutUint16(output[base+4:base+6], delay)
				pendingControl = true
			}
		case 0x2c:
			if !pendingControl {
				if string(output[:6]) == "GIF87a" {
					copy(output[:6], "GIF89a")
				}
				output = append(output, 0x21, 0xf9, 4, 0, byte(delay), byte(delay>>8), 0, 0)
			}
			packed := data[pos+8]
			pos += 9
			if packed&0x80 != 0 {
				pos += 3 * (1 << ((packed & 7) + 1))
			}
			pos++
			skipBlocks()
			output = append(output, data[start:pos]...)
			pendingControl = false
		case 0x3b:
			output = append(output, marker)
		default:
			return nil, errors.New("GIF 数据无效。")
		}
		if len(output) > MaxImageBytes {
			return nil, errors.New("设置帧率后的 GIF 超过 40 MB。")
		}
	}
	return output, nil
}
