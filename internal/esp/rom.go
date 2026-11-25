package esp

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/bits"
	"strings"
	"time"

	"go.bug.st/serial"

	"espcrypt/internal/serialx"
)

const (
	cmdFlashBegin    = 0x02
	cmdFlashData     = 0x03
	cmdFlashEnd      = 0x04
	cmdSync          = 0x08
	cmdWriteReg      = 0x09
	cmdReadReg       = 0x0A
	cmdSPISetParams  = 0x0B
	cmdSPIAttach     = 0x0D
	cmdReadFlashSlow = 0x0E
	cmdChangeBaud    = 0x0F
	cmdFlashDeflBeg  = 0x10
	cmdFlashDeflData = 0x11
	cmdFlashDeflEnd  = 0x12
	cmdSPIFlashMD5   = 0x13
	cmdSecurityInfo  = 0x14

	flashWriteSize = 0x400
	flashSector    = 0x1000
	checksumSeed   = 0xEF

	chipDetectMagicReg = 0x40001000
	c3MACEfuse         = 0x60008800 + 0x044
	c6MACEfuse         = 0x600B0800 + 0x044
	c6LPWDTBase        = 0x600B1C00
)

const (
	s3SPIBase    = 0x60002000
	c6SPIBase    = 0x60003000
	s3SPICmd     = s3SPIBase + 0x00
	s3SPIUsr     = s3SPIBase + 0x18
	s3SPIUsr1    = s3SPIBase + 0x1C
	s3SPIUsr2    = s3SPIBase + 0x20
	s3SPIMosiLen = s3SPIBase + 0x24
	s3SPIMisoLen = s3SPIBase + 0x28
	s3SPIW0      = s3SPIBase + 0x58
	s3MACEfuse   = 0x60007000 + 0x044

	s3RTCOption1 = 0x6000812C

	spiUsrCommand = 1 << 31
	spiUsrMiso    = 1 << 28
	spiUsrMosi    = 1 << 27
	spiCmdUsr     = 1 << 18
)

var chipMagic = map[uint32]string{
	0x00F01D83: "ESP32",
	0x000007C6: "ESP32-S2",
	0x00000009: "ESP32-S3",
	0x6921506F: "ESP32-C3",
	0x1B31506F: "ESP32-C3",
	0x2CE0806F: "ESP32-C6",
}

var romErrors = map[byte]string{
	0x05: "invalid command",
	0x06: "operation failed",
	0x07: "invalid checksum",
	0x08: "flash-schrijffout",
	0x09: "flash-leesfout",
	0x0A: "invalid read length",
	0x0B: "deflate-fout",
}

type Progress func(done, total int)

type Loader struct {
	port                  serial.Port
	rx                    []byte
	Chip                  string
	MAC                   string
	FlashID               uint32
	FlashLen              int
	SecurityInfoAvailable bool
	SecureBoot            bool
	FlashEncrypted        bool
	SecureDownload        bool
	log                   func(string)

	dead bool
}

const writeTimeout = 5 * time.Second

func (l *Loader) write(b []byte) error {
	if l.dead {
		return errors.New("serial port is gone (the chip reset or was unplugged)")
	}
	if err := serialx.Write(l.port, b, writeTimeout); err != nil {
		if errors.Is(err, serialx.ErrWriteTimeout) {
			l.dead = true
			return errors.New("serial write timed out (the chip reset or was unplugged)")
		}
		return err
	}
	return nil
}

type ResetMode string

const (
	ResetAuto    ResetMode = "auto"
	ResetManual  ResetMode = "manual"
	ResetUSBJTAG ResetMode = "usbjtag"
	ResetClassic ResetMode = "classic"
)

func Connect(portName string, mode ResetMode, log func(string)) (*Loader, error) {
	if log == nil {
		log = func(string) {}
	}
	p, err := serial.Open(portName, &serial.Mode{BaudRate: 115200})
	if err != nil {
		return nil, fmt.Errorf("opening port %s failed: %w", portName, err)
	}
	return connectPort(p, mode, log)
}

func connectPort(p serial.Port, mode ResetMode, log func(string)) (*Loader, error) {
	if log == nil {
		log = func(string) {}
	}
	l := &Loader{port: p, log: log}
	seqs := []ResetMode{mode}
	if mode == ResetAuto {
		seqs = []ResetMode{ResetUSBJTAG, ResetClassic, ResetManual}
	}
	var synced bool
	for _, s := range seqs {
		log("Resetting into download mode (" + string(s) + ")…")
		l.reset(s)
		if l.trySync(6) {
			synced = true
			break
		}
	}
	if !synced {
		p.Close()
		return nil, errors.New("no connection to the ROM bootloader. Hold BOOT, tap RESET, release BOOT and try again")
	}
	log("Connected to bootloader")
	if err := l.detect(); err != nil {
		p.Close()
		return nil, err
	}
	return l, nil
}

func (l *Loader) reset(mode ResetMode) {
	p := l.port
	sleep := func(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }
	switch mode {
	case ResetUSBJTAG:
		_ = p.SetRTS(false)
		_ = p.SetDTR(false)
		sleep(100)
		_ = p.SetDTR(true)
		_ = p.SetRTS(false)
		sleep(100)
		_ = p.SetRTS(true)
		_ = p.SetDTR(false)
		_ = p.SetRTS(true)
		sleep(100)
		_ = p.SetDTR(false)
		_ = p.SetRTS(false)
	case ResetClassic:
		_ = p.SetDTR(false)
		_ = p.SetRTS(true)
		sleep(100)
		_ = p.SetDTR(true)
		_ = p.SetRTS(false)
		sleep(50)
		_ = p.SetDTR(false)
	case ResetManual:
		sleep(50)
	}
	sleep(50)
	_ = p.ResetInputBuffer()
	l.rx = nil
}

func (l *Loader) trySync(attempts int) bool {
	payload := append([]byte{0x07, 0x07, 0x12, 0x20}, bytes.Repeat([]byte{0x55}, 32)...)
	for i := 0; i < attempts; i++ {
		if _, _, err := l.command(cmdSync, payload, 0, 150*time.Millisecond); err == nil {

			for j := 0; j < 8; j++ {
				if _, err := l.readFrame(50 * time.Millisecond); err != nil {
					break
				}
			}
			return true
		}
	}
	return false
}

func slipEncode(b []byte) []byte {
	out := []byte{0xC0}
	for _, c := range b {
		switch c {
		case 0xC0:
			out = append(out, 0xDB, 0xDC)
		case 0xDB:
			out = append(out, 0xDB, 0xDD)
		default:
			out = append(out, c)
		}
	}
	return append(out, 0xC0)
}

func (l *Loader) readFrame(timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 4096)
	for {
		if start := bytes.IndexByte(l.rx, 0xC0); start >= 0 {
			if end := bytes.IndexByte(l.rx[start+1:], 0xC0); end >= 0 {
				raw := l.rx[start+1 : start+1+end]
				l.rx = l.rx[start+1+end:]
				if len(raw) == 0 {
					continue
				}
				var out []byte
				for i := 0; i < len(raw); i++ {
					if raw[i] == 0xDB && i+1 < len(raw) {
						i++
						if raw[i] == 0xDC {
							out = append(out, 0xC0)
						} else {
							out = append(out, 0xDB)
						}
						continue
					}
					out = append(out, raw[i])
				}
				return out, nil
			}
		}
		left := time.Until(deadline)
		if left <= 0 {
			return nil, errors.New("timeout")
		}
		if left > 100*time.Millisecond {
			left = 100 * time.Millisecond
		}
		_ = l.port.SetReadTimeout(left)
		n, err := l.port.Read(buf)
		if err != nil {
			return nil, err
		}
		l.rx = append(l.rx, buf[:n]...)
	}
}

func (l *Loader) command(op byte, data []byte, chk uint32, timeout time.Duration) (uint32, []byte, error) {
	pkt := make([]byte, 8, 8+len(data))
	pkt[0] = 0x00
	pkt[1] = op
	binary.LittleEndian.PutUint16(pkt[2:], uint16(len(data)))
	binary.LittleEndian.PutUint32(pkt[4:], chk)
	pkt = append(pkt, data...)
	if err := l.write(slipEncode(pkt)); err != nil {
		return 0, nil, err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		f, err := l.readFrame(time.Until(deadline))
		if err != nil {
			return 0, nil, err
		}
		if len(f) < 8 || f[0] != 0x01 || f[1] != op {
			continue
		}
		val := binary.LittleEndian.Uint32(f[4:8])
		body := f[8:]
		statusLen := 4
		if len(body) < 4 {
			statusLen = 2
		}
		if len(body) < statusLen {
			return val, nil, nil
		}
		st := body[len(body)-statusLen:]
		if st[0] != 0 {
			msg := romErrors[st[1]]
			if msg == "" {
				msg = fmt.Sprintf("error 0x%02X", st[1])
			}
			return val, nil, fmt.Errorf("bootloader: %s", msg)
		}
		return val, body[:len(body)-statusLen], nil
	}
	return 0, nil, errors.New("timeout")
}

func le32(vals ...uint32) []byte {
	b := make([]byte, 4*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint32(b[i*4:], v)
	}
	return b
}

func (l *Loader) readReg(addr uint32) (uint32, error) {
	v, _, err := l.command(cmdReadReg, le32(addr), 0, 3*time.Second)
	return v, err
}

func (l *Loader) writeReg(addr, val uint32) error { return l.writeRegMask(addr, val, 0xFFFFFFFF) }

func (l *Loader) writeRegMask(addr, val, mask uint32) error {
	_, _, err := l.command(cmdWriteReg, le32(addr, val, mask, 0), 0, 3*time.Second)
	return err
}

func (l *Loader) detect() error {

	if _, info, err := l.command(cmdSecurityInfo, nil, 0, 3*time.Second); err == nil && len(info) >= 20 {
		flags := binary.LittleEndian.Uint32(info[:4])
		l.SecurityInfoAvailable = true
		l.SecureBoot = flags&(1<<0) != 0
		l.SecureDownload = flags&(1<<2) != 0
		l.FlashEncrypted = bits.OnesCount8(info[4])%2 == 1
		switch binary.LittleEndian.Uint32(info[12:16]) {
		case 9:
			l.Chip = "ESP32-S3"
		case 2:
			l.Chip = "ESP32-S2"
		case 5:
			l.Chip = "ESP32-C3"
		case 13:
			l.Chip = "ESP32-C6"
		}
	}
	if l.Chip == "" {
		magic, err := l.readReg(chipDetectMagicReg)
		if err != nil {
			return fmt.Errorf("chip not recognized: %w", err)
		}
		l.Chip = chipMagic[magic]
		if l.Chip == "" {
			l.Chip = fmt.Sprintf("unknown (0x%08X)", magic)
		}
	}
	l.log("Chip: " + l.Chip)
	if l.Chip != "ESP32-S3" && l.Chip != "ESP32-C3" && l.Chip != "ESP32-C6" {
		return fmt.Errorf("this tool supports ESP32-S3, ESP32-C3 and ESP32-C6 (found: %s)", l.Chip)
	}
	macReg := uint32(s3MACEfuse)
	switch l.Chip {
	case "ESP32-C3":
		macReg = c3MACEfuse
	case "ESP32-C6":
		macReg = c6MACEfuse
	}
	if m0, err := l.readReg(macReg); err == nil {
		if m1, err := l.readReg(macReg + 4); err == nil {
			b := []byte{byte(m1 >> 8), byte(m1), byte(m0 >> 24), byte(m0 >> 16), byte(m0 >> 8), byte(m0)}
			parts := make([]string, 6)
			for i, x := range b {
				parts[i] = fmt.Sprintf("%02X", x)
			}
			l.MAC = strings.Join(parts, ":")
		}
	}
	if _, _, err := l.command(cmdSPIAttach, make([]byte, 8), 0, 3*time.Second); err != nil {
		return fmt.Errorf("attaching SPI flash failed: %w", err)
	}
	if id, err := l.spiCommand(0x9F, nil, 24); err == nil && len(id) >= 3 {
		l.FlashID = uint32(id[0])<<16 | uint32(id[1])<<8 | uint32(id[2])
		if id[2] >= 0x10 && id[2] <= 0x1A {
			l.FlashLen = 1 << id[2]
		}
	}
	if l.FlashLen == 0 {
		l.FlashLen = 4 << 20
		l.log("Flash size not detected, assuming 4 MB")
	} else {
		l.log(fmt.Sprintf("Flash: %d MB (JEDEC %06X)", l.FlashLen>>20, l.FlashID))
	}

	params := le32(0, uint32(l.FlashLen), 64*1024, flashSector, 256, 0xFFFF)
	if _, _, err := l.command(cmdSPISetParams, params, 0, 3*time.Second); err != nil {
		return fmt.Errorf("setting SPI flash parameters failed: %w", err)
	}
	return nil
}

func (l *Loader) spiCommand(cmd byte, mosi []byte, readBits int) ([]byte, error) {

	off := uint32(0)
	if l.Chip == "ESP32-C6" {
		off = c6SPIBase - s3SPIBase
	}
	spiCmd, spiUsr, spiUsr1, spiUsr2 := s3SPICmd+off, s3SPIUsr+off, s3SPIUsr1+off, s3SPIUsr2+off
	spiMosiLen, spiMisoLen, spiW0 := s3SPIMosiLen+off, s3SPIMisoLen+off, s3SPIW0+off
	oldUsr, err := l.readReg(spiUsr)
	if err != nil {
		return nil, err
	}
	oldUsr2, _ := l.readReg(spiUsr2)
	defer func() {
		_ = l.writeReg(spiUsr, oldUsr)
		_ = l.writeReg(spiUsr2, oldUsr2)
	}()
	flags := uint32(spiUsrCommand)
	if readBits > 0 {
		flags |= spiUsrMiso
		if err := l.writeReg(spiMisoLen, uint32(readBits-1)); err != nil {
			return nil, err
		}
	}
	if len(mosi) > 0 {
		flags |= spiUsrMosi
		if err := l.writeReg(spiMosiLen, uint32(len(mosi)*8-1)); err != nil {
			return nil, err
		}
	}
	_ = l.writeReg(spiUsr1, 0)
	if err := l.writeReg(spiUsr, flags); err != nil {
		return nil, err
	}
	if err := l.writeReg(spiUsr2, 7<<28|uint32(cmd)); err != nil {
		return nil, err
	}
	padded := append(append([]byte{}, mosi...), make([]byte, (4-len(mosi)%4)%4)...)
	for i := 0; i < len(padded); i += 4 {
		if err := l.writeReg(spiW0+uint32(i), binary.LittleEndian.Uint32(padded[i:])); err != nil {
			return nil, err
		}
	}
	if err := l.writeReg(spiCmd, spiCmdUsr); err != nil {
		return nil, err
	}
	for i := 0; ; i++ {
		v, err := l.readReg(spiCmd)
		if err != nil {
			return nil, err
		}
		if v&spiCmdUsr == 0 {
			break
		}
		if i > 20 {
			return nil, errors.New("SPI command not responding")
		}
	}
	out := make([]byte, 0, (readBits+7)/8)
	for i := 0; i < (readBits+31)/32; i++ {
		v, err := l.readReg(spiW0 + uint32(i*4))
		if err != nil {
			return nil, err
		}
		out = binary.LittleEndian.AppendUint32(out, v)
	}
	return out[:(readBits+7)/8], nil
}

func (l *Loader) ChangeBaud(baud int) error {
	if _, _, err := l.command(cmdChangeBaud, le32(uint32(baud), 0), 0, 3*time.Second); err != nil {
		return err
	}
	if err := l.port.SetMode(&serial.Mode{BaudRate: baud}); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	_ = l.port.ResetInputBuffer()
	l.rx = nil
	return nil
}

func checksum(b []byte) uint32 {
	c := uint32(checksumSeed)
	for _, x := range b {
		c ^= uint32(x)
	}
	return c
}

func eraseTimeout(size int) time.Duration {
	t := time.Duration(size/(1<<20)+1) * 15 * time.Second
	if t < 10*time.Second {
		t = 10 * time.Second
	}
	return t
}

func (l *Loader) WriteFlash(data []byte, offset uint32, progress Progress) error {
	if int(offset)+len(data) > l.FlashLen {
		return fmt.Errorf("image (%d bytes at 0x%X) is larger than the flash (%d bytes)", len(data), offset, l.FlashLen)
	}
	if offset%flashSector != 0 {
		return errors.New("offset must be a multiple of 0x1000")
	}
	var comp bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&comp, zlib.BestCompression)
	zw.Write(data)
	zw.Close()
	c := comp.Bytes()
	blocks := (len(c) + flashWriteSize - 1) / flashWriteSize
	eraseSize := uint32((len(data) + flashSector - 1) / flashSector * flashSector)
	l.log(fmt.Sprintf("Erasing and writing: %d bytes (%d compressed) at 0x%X", len(data), len(c), offset))
	if _, _, err := l.command(cmdFlashDeflBeg, le32(eraseSize, uint32(blocks), flashWriteSize, offset, 0), 0, eraseTimeout(int(eraseSize))); err != nil {
		return fmt.Errorf("flash begin: %w", err)
	}
	for seq := 0; seq < blocks; seq++ {
		end := (seq + 1) * flashWriteSize
		if end > len(c) {
			end = len(c)
		}
		blk := c[seq*flashWriteSize : end]
		payload := append(le32(uint32(len(blk)), uint32(seq), 0, 0), blk...)
		var err error
		for try := 0; try < 3; try++ {
			if _, _, err = l.command(cmdFlashDeflData, payload, checksum(blk), 30*time.Second); err == nil {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("blok %d: %w", seq, err)
		}
		if progress != nil {
			progress(int(float64(seq+1)/float64(blocks)*float64(len(data))), len(data))
		}
	}
	l.log("Verifying (MD5)…")
	got, err := l.FlashMD5(offset, uint32(len(data)))
	if err != nil {
		return fmt.Errorf("verification: %w", err)
	}
	want := md5.Sum(data)
	if got != hex.EncodeToString(want[:]) {
		return fmt.Errorf("verification failed: MD5 %s, expected %s", got, hex.EncodeToString(want[:]))
	}
	l.log("Verification passed")
	return nil
}

func (l *Loader) FlashMD5(offset, size uint32) (string, error) {
	_, body, err := l.command(cmdSPIFlashMD5, le32(offset, size, 0, 0), 0, eraseTimeout(int(size))+10*time.Second)
	if err != nil {
		return "", err
	}
	switch len(body) {
	case 32:
		return strings.ToLower(string(body)), nil
	case 16:
		return hex.EncodeToString(body), nil
	}
	return "", fmt.Errorf("onverwacht MD5-antwoord (%d bytes)", len(body))
}

func (l *Loader) HardReset() {
	if err := l.WatchdogReset(); err == nil {

		time.Sleep(500 * time.Millisecond)
		return
	}
	if l.dead {
		return
	}
	_ = l.port.SetDTR(false)
	_ = l.port.SetRTS(true)
	time.Sleep(150 * time.Millisecond)
	_ = l.port.SetRTS(false)
}

func (l *Loader) Close() { _ = l.port.Close() }

func (l *Loader) WatchdogReset() error {
	const (
		rtcBase = 0x60008000
		wdtWKey = 0x50D83AA1
	)
	wdtCfg0, wdtCfg1, wdtProt := uint32(rtcBase+0x98), uint32(rtcBase+0x9C), uint32(rtcBase+0xB0)
	switch l.Chip {
	case "ESP32-C3":
		wdtCfg0, wdtCfg1, wdtProt = rtcBase+0x90, rtcBase+0x94, rtcBase+0xA8
	case "ESP32-C6":
		wdtCfg0, wdtCfg1, wdtProt = c6LPWDTBase+0x00, c6LPWDTBase+0x04, c6LPWDTBase+0x18
	default:
		_ = l.writeRegMask(s3RTCOption1, 0, 0x1)
	}
	if err := l.writeReg(wdtProt, wdtWKey); err != nil {
		return err
	}

	_ = l.writeReg(wdtCfg1, 5000)
	_ = l.writeReg(wdtCfg0, 1<<31|5<<28|1<<8|2)
	return nil
}
