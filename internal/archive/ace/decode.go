package ace

import "fmt"

type decoder struct{ lz *lz77 }

// dicSize 由標頭的 params 欄位算出這個成員要多大的字典。
func dicSize(params uint16) int {
	bits := int(params&0x0F) + minDicBits
	if bits > maxDicBits {
		bits = maxDicBits
	}
	return 1 << bits
}

func (d *decoder) member(data []byte, h *header) (File, error) {
	f := File{
		Name: h.name, Size: h.origSize, ModTime: h.modTime,
		CRC32: h.crc32, CompType: h.compType,
		IsDir: h.attribs&0x10 != 0,
	}
	switch {
	case h.flags&flagPassword != 0:
		return f, fmt.Errorf("有密碼,還不支援")
	case h.flags&(flagContPrev|flagContNext) != 0:
		return f, fmt.Errorf("跨片壓縮檔,還不支援")
	}
	if f.IsDir || h.origSize == 0 {
		return f, nil
	}
	if h.dataAt+int(h.packSize) > len(data) {
		return f, fmt.Errorf("資料被截斷")
	}
	body := data[h.dataAt : h.dataAt+int(h.packSize)]

	d.lz.setDicSize(dicSize(h.params))
	var out []byte
	var err error
	switch h.compType {
	case compStored:
		if int64(len(body)) < h.origSize {
			return f, fmt.Errorf("不壓縮的資料只有 %d 個位元組,期望 %d", len(body), h.origSize)
		}
		out = body[:h.origSize]
		d.lz.register(out)
	case compLZ77:
		out, err = d.decodeLZ77(body, h.origSize)
	case compBlocked:
		out, err = d.decodeBlocked(body, h.origSize)
	default:
		err = fmt.Errorf("還不支援壓縮法 %d", h.compType)
	}
	if err != nil {
		return f, err
	}
	f.Data = out
	if got := aceCRC32(out); got != h.crc32 {
		return f, fmt.Errorf("CRC 不符(算出 %08X,標頭寫 %08X)", got, h.crc32)
	}
	return f, nil
}

// decodeLZ77 是 ACE 1.0 的路徑:整份就是一段 LZ77,沒有模式切換。
func (d *decoder) decodeLZ77(body []byte, size int64) ([]byte, error) {
	b := newBitStream(body)
	d.lz.reinit()
	var out []byte
	for int64(len(out)) < size {
		chunk, next, err := d.lz.read(b, int(size)-len(out))
		if err != nil {
			return out, err
		}
		if next != nil {
			return out, fmt.Errorf("ACE 1.0 的 LZ77 不該出現模式切換碼")
		}
		if len(chunk) == 0 {
			return out, fmt.Errorf("沒有進展,可能是壞檔")
		}
		out = append(out, chunk...)
	}
	return out, nil
}

// decodeBlocked 是 ACE 2.0 的路徑:資料切成一段一段,每段前面可以插入
// 一個模式切換碼,換成別的前處理方式。
func (d *decoder) decodeBlocked(body []byte, size int64) ([]byte, error) {
	b := newBitStream(body)
	d.lz.reinit()

	var out []byte
	var exeLeftover []byte
	lastDelta := byte(0)
	mode := &aceMode{mode: modeLZ77}
	var next *aceMode

	for int64(len(out)) < size {
		if next != nil {
			mode, next = next, nil
		}
		var chunk []byte
		var err error

		switch mode.mode {
		case modeLZ77Delta:
			chunk, next, err = d.readDelta(b, mode, &lastDelta)
			if err != nil {
				return out, err
			}
			if len(chunk) == 0 && next != nil {
				continue
			}

		case modeLZ77, modeLZ77Exe:
			var buf []byte
			if len(exeLeftover) > 0 {
				buf = append(buf, exeLeftover...)
				exeLeftover = nil
			}
			want := int(size) - len(out) - len(buf)
			if want <= 0 {
				return out, fmt.Errorf("沒有進展,可能是壞檔")
			}
			c, nm, err := d.lz.read(b, want)
			if err != nil {
				return out, err
			}
			next = nm
			buf = append(buf, c...)
			if mode.mode == modeLZ77Exe {
				buf, exeLeftover = patchExe(buf, len(out), mode.exeMode)
			}
			chunk = buf

		case modeSound8, modeSound16, modeSound32A, modeSound32B, modePic:
			return out, fmt.Errorf("還不支援 %s 模式", modeName(mode.mode))

		default:
			return out, fmt.Errorf("認不得的模式 %d", mode.mode)
		}

		if len(chunk) == 0 && next == nil {
			return out, fmt.Errorf("沒有進展,可能是壞檔")
		}
		out = append(out, chunk...)
	}
	return out, nil
}

// readDelta 解 LZ77_DELTA:先解出一整塊差值資料,累加還原成位元組值,
// 再依 delta_dist 把交錯存放的幾個平面重新編織回原本的順序。
func (d *decoder) readDelta(b *bitStream, mode *aceMode, last *byte) ([]byte, *aceMode, error) {
	var next *aceMode
	delta := make([]byte, 0, mode.deltaLen)
	for len(delta) < mode.deltaLen {
		chunk, nm, err := d.lz.read(b, mode.deltaLen-len(delta))
		if err != nil {
			return nil, nil, err
		}
		delta = append(delta, chunk...)
		if nm != nil {
			if next != nil {
				return nil, nil, fmt.Errorf("DELTA 區塊裡出現兩個模式切換碼")
			}
			next = nm
			if len(delta) == 0 {
				return nil, next, nil
			}
			break
		}
		if len(chunk) == 0 {
			return nil, nil, fmt.Errorf("DELTA 沒有進展")
		}
	}
	for i := range delta {
		delta[i] += *last
		*last = delta[i]
	}
	if mode.deltaDist == 0 {
		return nil, nil, fmt.Errorf("DELTA 的 dist 是 0")
	}
	planeSize := mode.deltaLen / mode.deltaDist
	out := make([]byte, 0, len(delta))
	for pos := 0; pos < planeSize; pos++ {
		for p := 0; p < mode.deltaLen; p += planeSize {
			if p+pos < len(delta) {
				out = append(out, delta[p+pos])
			}
		}
	}
	return out, next, nil
}

// patchExe 還原 LZ77_EXE 的前處理:編碼時把 x86 的 CALL/JMP 目標改成
// 相對於檔案位移的值,解碼時要減回去。
//
// 回傳處理過的資料,以及尾端最多 4 個位元組的殘留 —— 一條機器指令
// 可能跨在兩個區塊的交界上,opcode 在這一段、位移在下一段。
func patchExe(buf []byte, filePos int, exeMode int) ([]byte, []byte) {
	i := 0
	for ; i < len(buf); i++ {
		if i+4 >= len(buf) {
			break
		}
		switch buf[i] {
		case 0xE8: // CALL rel16/rel32
			// 位移的寬度是格式定的(16 或 32 位元),要用固定寬度的型別算。
			//
			// [雷] 寫成 `int` 再 `& 0xFFFFFFFF` 在 64 位元機器上「剛好對」,
			// 但在 32 位元平台(Android 的 armeabi-v7a)連編都編不過 ——
			// 0xFFFFFFFF 當 untyped 常數塞不進 32 位元的 int。
			// 就算編得過,那個遮罩在 32 位元上也是沒有作用的空操作。
			pos := uint32(filePos + i)
			if exeMode == 0 {
				rel := uint16(buf[i+1]) | uint16(buf[i+2])<<8
				rel -= uint16(pos)
				buf[i+1], buf[i+2] = byte(rel), byte(rel>>8)
				i += 2
			} else {
				rel := uint32(buf[i+1]) | uint32(buf[i+2])<<8 |
					uint32(buf[i+3])<<16 | uint32(buf[i+4])<<24
				rel -= pos
				buf[i+1], buf[i+2] = byte(rel), byte(rel>>8)
				buf[i+3], buf[i+4] = byte(rel>>16), byte(rel>>24)
				i += 4
			}
		case 0xE9: // JMP rel16
			pos := uint16(filePos + i)
			rel := uint16(buf[i+1]) | uint16(buf[i+2])<<8
			rel -= pos
			buf[i+1], buf[i+2] = byte(rel), byte(rel>>8)
			i += 2
		}
	}
	for ; i < len(buf); i++ {
		if buf[i] == 0xE8 || buf[i] == 0xE9 {
			return buf[:i], append([]byte(nil), buf[i:]...)
		}
	}
	return buf, nil
}
