//go:build ignore

package main

import (
	"archive/zip"
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

const (
	appName     = "Pyestro Studio"
	version     = "0.1.0"
	description = "Maestro-compatible visual inspector & YAML runner for Pyestro and Pyestro Web."
)

func main() {
	defaultArch := "amd64"
	if runtime.GOOS == "windows" {
		defaultArch = runtime.GOARCH
	}

	targetArch := flag.String("arch", defaultArch, "Target Windows GOARCH (amd64, arm64, 386)")
	makeZip := flag.Bool("zip", false, "Also package a portable .zip archive alongside the .exe")
	consoleMode := flag.Bool("console", false, "Keep Windows console window visible (omit -H=windowsgui)")
	keepSyso := flag.Bool("keep-syso", false, "Keep generated .syso resource file after building")
	flag.Parse()

	machine, relocType, archLabel, err := windowsTarget(*targetArch)
	if err != nil {
		fatalf("%v", err)
	}

	baseName := strings.ReplaceAll(appName, " ", "_")
	outputExe := fmt.Sprintf("%s-%s-%s.exe", baseName, version, archLabel)
	sysoPath := fmt.Sprintf("rsrc_windows_%s.syso", *targetArch)

	fmt.Printf("🪟 Building Windows executable: %s (GOOS=windows GOARCH=%s)\n", outputExe, *targetArch)

	_ = os.Remove(outputExe)
	_ = os.Remove(sysoPath)

	fmt.Println("🎨 Preparing Windows icon & application manifest resources...")
	rsrcImages, err := loadIconImages()
	if err != nil {
		fatalf("failed to prepare Windows icon: %v", err)
	}

	sysoBytes, err := buildResourceSyso(machine, relocType, rsrcImages, buildManifestXML())
	if err != nil {
		fatalf("failed to build Windows resource .syso: %v", err)
	}
	if err := os.WriteFile(sysoPath, sysoBytes, 0o644); err != nil {
		fatalf("failed to write %s: %v", sysoPath, err)
	}
	if !*keepSyso {
		defer os.Remove(sysoPath)
	}

	ldflags := "-s -w -H=windowsgui"
	if *consoleMode {
		ldflags = "-s -w"
	}

	fmt.Println("🔨 Compiling Go binary (CGO_ENABLED=0)...")
	buildCmd := exec.Command("go", "build", "-trimpath", "-ldflags="+ldflags, "-o", outputExe, ".")
	buildCmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS=windows",
		"GOARCH="+*targetArch,
	)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fatalf("go build failed: %s (%v)", string(out), err)
	}

	fmt.Printf("✅ Successfully generated: %s\n", outputExe)

	if *makeZip {
		zipPath := fmt.Sprintf("%s-%s-%s-windows.zip", baseName, version, archLabel)
		fmt.Printf("📦 Packaging portable archive: %s\n", zipPath)
		if err := createPortableZip(zipPath, outputExe, appName+".exe"); err != nil {
			fatalf("failed to create zip archive: %v", err)
		}
		fmt.Printf("✅ Successfully generated: %s\n", zipPath)
	}
}

func windowsTarget(goarch string) (machine uint16, relocType uint16, archLabel string, err error) {
	switch goarch {
	case "amd64":
		return 0x8664, 0x0003, "x86_64", nil // IMAGE_FILE_MACHINE_AMD64, IMAGE_REL_AMD64_ADDR32NB
	case "arm64":
		return 0xAA64, 0x0002, "arm64", nil // IMAGE_FILE_MACHINE_ARM64, IMAGE_REL_ARM64_ADDR32NB
	case "386":
		return 0x014C, 0x0007, "x86", nil // IMAGE_FILE_MACHINE_I386, IMAGE_REL_I386_DIR32NB
	default:
		return 0, 0, "", fmt.Errorf("unsupported Windows architecture %q (expected amd64, arm64, or 386)", goarch)
	}
}

type iconEntry struct {
	width  int
	height int
	data   []byte
}

func loadIconImages() ([]iconEntry, error) {
	icoPath := filepath.Join("assets", "app.ico")
	if raw, err := os.ReadFile(icoPath); err == nil {
		entries, parseErr := parseICO(raw)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid %s: %w", icoPath, parseErr)
		}
		return entries, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	pngPath := filepath.Join("assets", "icon.png")
	f, err := os.Open(pngPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", pngPath, err)
	}
	defer f.Close()

	srcImg, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", pngPath, err)
	}

	srcNRGBA := toNRGBA(srcImg)
	sizes := []int{16, 32, 48, 256}
	entries := make([]iconEntry, 0, len(sizes))
	for _, size := range sizes {
		resized := resizeNRGBA(srcNRGBA, size, size)
		var payload []byte
		if size >= 256 {
			var buf bytes.Buffer
			if err := png.Encode(&buf, resized); err != nil {
				return nil, fmt.Errorf("encode %dx%d PNG icon: %w", size, size, err)
			}
			payload = buf.Bytes()
		} else {
			payload = encodeDIBIcon(resized)
		}
		entries = append(entries, iconEntry{
			width:  size,
			height: size,
			data:   payload,
		})
	}
	return entries, nil
}

func parseICO(raw []byte) ([]iconEntry, error) {
	if len(raw) < 6 {
		return nil, errors.New("ICO file too short")
	}
	reserved := binary.LittleEndian.Uint16(raw[0:2])
	typ := binary.LittleEndian.Uint16(raw[2:4])
	count := int(binary.LittleEndian.Uint16(raw[4:6]))
	if reserved != 0 || typ != 1 || count == 0 {
		return nil, errors.New("not a valid ICO header")
	}
	headerEnd := 6 + count*16
	if len(raw) < headerEnd {
		return nil, errors.New("truncated ICO directory")
	}
	entries := make([]iconEntry, 0, count)
	for i := range count {
		off := 6 + i*16
		w := int(raw[off])
		if w == 0 {
			w = 256
		}
		h := int(raw[off+1])
		if h == 0 {
			h = 256
		}
		size := int(binary.LittleEndian.Uint32(raw[off+8 : off+12]))
		imgOff := int(binary.LittleEndian.Uint32(raw[off+12 : off+16]))
		if imgOff < 0 || size <= 0 || imgOff+size > len(raw) {
			return nil, fmt.Errorf("invalid ICO entry %d bounds", i)
		}
		entries = append(entries, iconEntry{
			width:  w,
			height: h,
			data:   bytes.Clone(raw[imgOff : imgOff+size]),
		})
	}
	return entries, nil
}

func toNRGBA(src image.Image) *image.NRGBA {
	if nrgba, ok := src.(*image.NRGBA); ok && nrgba.Rect.Min == (image.Point{}) {
		return nrgba
	}
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			dst.Set(x, y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func resizeNRGBA(src *image.NRGBA, dstW, dstH int) *image.NRGBA {
	srcW := src.Rect.Dx()
	srcH := src.Rect.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	if srcW == 0 || srcH == 0 {
		return dst
	}

	for dy := range dstH {
		sy := (float64(dy)+0.5)*float64(srcH)/float64(dstH) - 0.5
		sy = max(0, min(sy, float64(srcH-1)))
		y0 := int(sy)
		y1 := min(y0+1, srcH-1)
		wy := sy - float64(y0)

		for dx := range dstW {
			sx := (float64(dx)+0.5)*float64(srcW)/float64(dstW) - 0.5
			sx = max(0, min(sx, float64(srcW-1)))
			x0 := int(sx)
			x1 := min(x0+1, srcW-1)
			wx := sx - float64(x0)

			c00 := src.NRGBAAt(x0, y0)
			c10 := src.NRGBAAt(x1, y0)
			c01 := src.NRGBAAt(x0, y1)
			c11 := src.NRGBAAt(x1, y1)

			// Interpolate premultiplied channels to avoid dark fringes on transparent edges.
			a00, a10 := float64(c00.A), float64(c10.A)
			a01, a11 := float64(c01.A), float64(c11.A)

			w00 := (1 - wx) * (1 - wy)
			w10 := wx * (1 - wy)
			w01 := (1 - wx) * wy
			w11 := wx * wy

			a := a00*w00 + a10*w10 + a01*w01 + a11*w11
			if a <= 0.5 {
				continue
			}

			r := (float64(c00.R)*a00*w00 + float64(c10.R)*a10*w10 + float64(c01.R)*a01*w01 + float64(c11.R)*a11*w11) / a
			g := (float64(c00.G)*a00*w00 + float64(c10.G)*a10*w10 + float64(c01.G)*a01*w01 + float64(c11.G)*a11*w11) / a
			b := (float64(c00.B)*a00*w00 + float64(c10.B)*a10*w10 + float64(c01.B)*a01*w01 + float64(c11.B)*a11*w11) / a

			off := dst.PixOffset(dx, dy)
			dst.Pix[off+0] = clampUint8(r)
			dst.Pix[off+1] = clampUint8(g)
			dst.Pix[off+2] = clampUint8(b)
			dst.Pix[off+3] = clampUint8(a)
		}
	}
	return dst
}

func clampUint8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// encodeDIBIcon builds a 32-bit BGRA BITMAPINFOHEADER + bottom-up pixels + 1-bpp AND mask.
func encodeDIBIcon(img *image.NRGBA) []byte {
	w := img.Rect.Dx()
	h := img.Rect.Dy()
	andStride := ((w + 31) / 32) * 4
	xorSize := w * h * 4
	andSize := andStride * h

	buf := make([]byte, 40+xorSize+andSize)
	binary.LittleEndian.PutUint32(buf[0:4], 40)           // biSize
	binary.LittleEndian.PutUint32(buf[4:8], uint32(w))    // biWidth
	binary.LittleEndian.PutUint32(buf[8:12], uint32(h*2)) // biHeight (XOR + AND)
	binary.LittleEndian.PutUint16(buf[12:14], 1)          // biPlanes
	binary.LittleEndian.PutUint16(buf[14:16], 32)         // biBitCount
	binary.LittleEndian.PutUint32(buf[20:24], uint32(xorSize+andSize))

	pos := 40
	for y := h - 1; y >= 0; y-- {
		for x := range w {
			c := img.NRGBAAt(x, y)
			buf[pos+0] = c.B
			buf[pos+1] = c.G
			buf[pos+2] = c.R
			buf[pos+3] = c.A
			pos += 4
		}
	}
	return buf
}

func buildGroupIconDir(entries []iconEntry) []byte {
	buf := make([]byte, 6+len(entries)*14)
	binary.LittleEndian.PutUint16(buf[0:2], 0)
	binary.LittleEndian.PutUint16(buf[2:4], 1)
	binary.LittleEndian.PutUint16(buf[4:6], uint16(len(entries)))

	for i, entry := range entries {
		off := 6 + i*14
		var wByte, hByte uint8
		if entry.width < 256 {
			wByte = uint8(entry.width)
		}
		if entry.height < 256 {
			hByte = uint8(entry.height)
		}
		buf[off+0] = wByte
		buf[off+1] = hByte
		buf[off+2] = 0                                      // bColorCount
		buf[off+3] = 0                                      // bReserved
		binary.LittleEndian.PutUint16(buf[off+4:off+6], 1)  // wPlanes
		binary.LittleEndian.PutUint16(buf[off+6:off+8], 32) // wBitCount
		binary.LittleEndian.PutUint32(buf[off+8:off+12], uint32(len(entry.data)))
		binary.LittleEndian.PutUint16(buf[off+12:off+14], uint16(i+1)) // nID -> RT_ICON ID
	}
	return buf
}

func buildManifestXML() []byte {
	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0" xmlns:asmv3="urn:schemas-microsoft-com:asm.v3">
  <assemblyIdentity version="%s.0" processorArchitecture="*" name="PyestroStudio" type="win32"/>
  <description>%s</description>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <asmv3:application>
    <asmv3:windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2, PerMonitor</dpiAwareness>
    </asmv3:windowsSettings>
  </asmv3:application>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
      <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
      <supportedOS Id="{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}"/>
      <supportedOS Id="{35138b9a-5d96-4fbd-8e2d-a2440225f93a}"/>
    </application>
  </compatibility>
</assembly>
`, version, xmlEscape(description))
	return []byte(xml)
}

func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

type rsrcItem struct {
	id   uint32
	data []byte
}

type rsrcTypeGroup struct {
	typeID uint32
	items  []rsrcItem
}

// buildResourceSyso constructs a COFF object (.syso) containing a .rsrc section
// with RT_ICON (3), RT_GROUP_ICON (14), and RT_MANIFEST (24).
func buildResourceSyso(machine uint16, relocType uint16, icons []iconEntry, manifest []byte) ([]byte, error) {
	if len(icons) == 0 {
		return nil, errors.New("at least one icon entry is required")
	}

	iconItems := make([]rsrcItem, 0, len(icons))
	for i, ic := range icons {
		iconItems = append(iconItems, rsrcItem{
			id:   uint32(i + 1),
			data: ic.data,
		})
	}

	groups := []rsrcTypeGroup{
		{typeID: 3, items: iconItems},                                            // RT_ICON
		{typeID: 14, items: []rsrcItem{{id: 1, data: buildGroupIconDir(icons)}}}, // RT_GROUP_ICON
		{typeID: 24, items: []rsrcItem{{id: 1, data: manifest}}},                 // RT_MANIFEST
	}
	slices.SortFunc(groups, func(a, b rsrcTypeGroup) int {
		return cmp.Compare(a.typeID, b.typeID)
	})
	for i := range groups {
		slices.SortFunc(groups[i].items, func(a, b rsrcItem) int {
			return cmp.Compare(a.id, b.id)
		})
	}

	const (
		dirHeaderSize = 16
		dirEntrySize  = 8
		dataEntrySize = 16
		langUS        = 0x0409
	)

	// Calculate directory layout offsets within .rsrc.
	rootDirOff := 0
	typeDirOffs := make([]int, len(groups))
	cursor := dirHeaderSize + len(groups)*dirEntrySize

	langDirOffs := make([][]int, len(groups))
	for i, g := range groups {
		typeDirOffs[i] = cursor
		cursor += dirHeaderSize + len(g.items)*dirEntrySize
		langDirOffs[i] = make([]int, len(g.items))
	}

	for i, g := range groups {
		for j := range g.items {
			langDirOffs[i][j] = cursor
			cursor += dirHeaderSize + dirEntrySize
		}
	}

	dataEntryOffs := make([][]int, len(groups))
	for i, g := range groups {
		dataEntryOffs[i] = make([]int, len(g.items))
		for j := range g.items {
			dataEntryOffs[i][j] = cursor
			cursor += dataEntrySize
		}
	}

	payloadOffs := make([][]int, len(groups))
	for i, g := range groups {
		payloadOffs[i] = make([]int, len(g.items))
		for j, item := range g.items {
			cursor = align4(cursor)
			payloadOffs[i][j] = cursor
			cursor += len(item.data)
		}
	}
	rsrcSize := align4(cursor)
	rsrc := make([]byte, rsrcSize)

	// Write root directory (Level 1: Type).
	writeResDirHeader(rsrc[rootDirOff:], uint16(len(groups)))
	for i, g := range groups {
		entOff := rootDirOff + dirHeaderSize + i*dirEntrySize
		binary.LittleEndian.PutUint32(rsrc[entOff:entOff+4], g.typeID)
		binary.LittleEndian.PutUint32(rsrc[entOff+4:entOff+8], 0x80000000|uint32(typeDirOffs[i]))
	}

	// Write Level 2 (Name/ID) and Level 3 (Language) directories + DataEntries + payloads.
	var relocOffsets []uint32
	for i, g := range groups {
		writeResDirHeader(rsrc[typeDirOffs[i]:], uint16(len(g.items)))
		for j, item := range g.items {
			typeEntOff := typeDirOffs[i] + dirHeaderSize + j*dirEntrySize
			binary.LittleEndian.PutUint32(rsrc[typeEntOff:typeEntOff+4], item.id)
			binary.LittleEndian.PutUint32(rsrc[typeEntOff+4:typeEntOff+8], 0x80000000|uint32(langDirOffs[i][j]))

			writeResDirHeader(rsrc[langDirOffs[i][j]:], 1)
			langEntOff := langDirOffs[i][j] + dirHeaderSize
			binary.LittleEndian.PutUint32(rsrc[langEntOff:langEntOff+4], langUS)
			binary.LittleEndian.PutUint32(rsrc[langEntOff+4:langEntOff+8], uint32(dataEntryOffs[i][j]))

			deOff := dataEntryOffs[i][j]
			binary.LittleEndian.PutUint32(rsrc[deOff:deOff+4], uint32(payloadOffs[i][j]))
			binary.LittleEndian.PutUint32(rsrc[deOff+4:deOff+8], uint32(len(item.data)))
			relocOffsets = append(relocOffsets, uint32(deOff))

			copy(rsrc[payloadOffs[i][j]:], item.data)
		}
	}

	// Assemble COFF object file.
	const (
		coffHeaderSize    = 20
		sectionHeaderSize = 40
		relocSize         = 10
		symbolSize        = 18
		scnCntInitData    = 0x00000040
		scnMemRead        = 0x40000000
		symClassStatic    = 3
	)

	rawDataOff := coffHeaderSize + sectionHeaderSize
	relocTableOff := rawDataOff + len(rsrc)
	symTableOff := relocTableOff + len(relocOffsets)*relocSize
	totalSize := symTableOff + symbolSize + 4

	coff := make([]byte, totalSize)

	// IMAGE_FILE_HEADER
	binary.LittleEndian.PutUint16(coff[0:2], machine)
	binary.LittleEndian.PutUint16(coff[2:4], 1) // NumberOfSections
	binary.LittleEndian.PutUint32(coff[8:12], uint32(symTableOff))
	binary.LittleEndian.PutUint32(coff[12:16], 1) // NumberOfSymbols

	// IMAGE_SECTION_HEADER (.rsrc)
	sh := coff[coffHeaderSize:]
	copy(sh[0:8], ".rsrc\x00\x00\x00")
	binary.LittleEndian.PutUint32(sh[16:20], uint32(len(rsrc)))
	binary.LittleEndian.PutUint32(sh[20:24], uint32(rawDataOff))
	binary.LittleEndian.PutUint32(sh[24:28], uint32(relocTableOff))
	binary.LittleEndian.PutUint16(sh[32:34], uint16(len(relocOffsets)))
	binary.LittleEndian.PutUint32(sh[36:40], scnCntInitData|scnMemRead)

	// .rsrc raw data
	copy(coff[rawDataOff:], rsrc)

	// IMAGE_RELOCATION entries
	for i, rvaOff := range relocOffsets {
		off := relocTableOff + i*relocSize
		binary.LittleEndian.PutUint32(coff[off:off+4], rvaOff)
		binary.LittleEndian.PutUint32(coff[off+4:off+8], 0) // SymbolTableIndex: .rsrc
		binary.LittleEndian.PutUint16(coff[off+8:off+10], relocType)
	}

	// COFF Symbol (.rsrc section symbol)
	sym := coff[symTableOff:]
	copy(sym[0:8], ".rsrc\x00\x00\x00")
	binary.LittleEndian.PutUint16(sym[12:14], 1) // SectionNumber (1-based)
	sym[16] = symClassStatic

	// Empty COFF string table (4-byte size = 4)
	binary.LittleEndian.PutUint32(coff[symTableOff+symbolSize:], 4)

	return coff, nil
}

func writeResDirHeader(dst []byte, idEntryCount uint16) {
	// Characteristics (4), TimeDateStamp (4), MajorVersion (2), MinorVersion (2),
	// NumberOfNamedEntries (2), NumberOfIdEntries (2).
	binary.LittleEndian.PutUint16(dst[14:16], idEntryCount)
}

func align4(n int) int {
	return (n + 3) &^ 3
}

func createPortableZip(zipPath, exePath, exeArchiveName string) error {
	_ = os.Remove(zipPath)
	outFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	zw := zip.NewWriter(outFile)
	if err := addFileToZip(zw, exePath, exeArchiveName); err != nil {
		_ = zw.Close()
		return err
	}
	if _, err := os.Stat("LICENSE"); err == nil {
		if err := addFileToZip(zw, "LICENSE", "LICENSE"); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}

func addFileToZip(zw *zip.Writer, srcPath, archiveName string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = archiveName
	hdr.Method = zip.Deflate

	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(w, f)
	return err
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "❌ Error: "+format+"\n", args...)
	os.Exit(1)
}
