package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf16"
	"howett.net/plist"
)

// The Python code uses a lot of warnings and yields.
// We'll just print warnings to stderr for simplicity.
func warn(msg string) {
	fmt.Fprintln(os.Stderr, "Warning:", msg)
}

// show_date: In Python code, it converts a 1904-based timestamp.
// In Python:
//   date = datetime.datetime(1904,1,1) + (timestamp since 1904)
// The DS_Store uses Mac epoch starting in 1904. We'll replicate that logic.
func showDate(timestamp float64) string {
	// Mac epoch (1904-01-01)
	macEpoch := time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC)
	date := macEpoch.Add(time.Duration(timestamp) * time.Second)
	// Format similar to Python code: '%B %-d, %Y at %-I:%M %p'
	// In Go we can do: "January 2, 2006 at 3:04 PM"
	return date.Format("January 2, 2006 at 3:04 PM")
}

func isDecimal(b []byte) bool {
	for _, c := range b {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func showBytes(data []byte) string {
	if len(data) >= 8 && bytes.HasPrefix(data, []byte("bplist")) && isDecimal(data[6:8]) {
		var val interface{}
		decoder := plist.NewDecoder(bytes.NewReader(data))
		if err := decoder.Decode(&val); err != nil {
			// If plist decoding fails, just return hex
			return fmt.Sprintf("0x%s", hex.EncodeToString(data))
		}
		return strings.Join(show(val, 0), "\n")
	} else if len(data) >= 4 && bytes.HasPrefix(data, []byte("book")) {
		// macOS alias type (unparsed)
		return fmt.Sprintf("(in macOS alias type, unparsed) %q", data)
	} else if len(data) >= 4 && bytes.HasPrefix(data, []byte("Bud1")) {
		// The python code tries to parse a DSStore from data.
		// We'll just note this as unparsed.
		embedded := []byte{0x00, 0x00, 0x00, 0x01}
		embedded = append(embedded, data...)
		ds := NewDSStore(embedded)
		if err := ds.Parse(); err == nil {
			var lines []string
			for _, r := range ds.records {
				lines = append(lines, r.humanReadable()...)
			}
			return strings.Join(lines, "\n")
		}
		// If parsing fails, just return hex
		return fmt.Sprintf("0x%s", hex.EncodeToString(data))
	} else {
		return fmt.Sprintf("0x%s", hex.EncodeToString(data))
	}
}

func isInline(data interface{}) bool {
	switch data.(type) {
	case string, bool, int, int64, float64, []byte:
		return true
	default:
		return false
	}
}

func showOne(data interface{}) string {
	lines := show(data, 0)
	if len(lines) > 0 {
		return lines[0]
	}
	return ""
}

func show(data interface{}, tabDepth int) []string {
	var result []string
	tabs := strings.Repeat("\t", tabDepth)

	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			if isInline(value) {
				result = append(result, fmt.Sprintf("%s%s: %s", tabs, key, showOne(value)))
			} else {
				result = append(result, fmt.Sprintf("%s%s:", tabs, key))
				result = append(result, show(value, tabDepth+1)...)
			}
		}
	case []interface{}:
		for _, value := range v {
			if isInline(value) {
				result = append(result, fmt.Sprintf("%s- %s", tabs, showOne(value)))
			} else {
				result = append(result, fmt.Sprintf("%s-", tabs))
				result = append(result, show(value, tabDepth+1)...)
			}
		}
	case []byte:
		result = append(result, fmt.Sprintf("%s%s", tabs, showBytes(v)))
	case bool:
		result = append(result, fmt.Sprintf("%s%v", tabs, v))
	case int:
		result = append(result, fmt.Sprintf("%s%d", tabs, v))
	case int64:
		result = append(result, fmt.Sprintf("%s%d", tabs, v))
	case float64:
		result = append(result, fmt.Sprintf("%s%f", tabs, v))
	case string:
		result = append(result, fmt.Sprintf("%s%s", tabs, v))
	default:
		// fallback
		result = append(result, fmt.Sprintf("%s%#v", tabs, v))
	}
	return result
}

// Record struct
type Record struct {
	name   string
	fields map[string]interface{}
}

func NewRecord(name string) *Record {
	return &Record{name: name, fields: make(map[string]interface{})}
}

func (r *Record) update(fields map[string]interface{}) {
	for k, v := range fields {
		r.fields[k] = v
	}
}

// Validate the type (we do best effort checks)
func (r *Record) validateType(field string, data interface{}, expected string, acceptableLengths ...int) {
	checkLen := func(d []byte) {
		if len(acceptableLengths) > 0 {
			okLen := false
			for _, al := range acceptableLengths {
				if len(d) == al {
					okLen = true
					break
				}
			}
			if !okLen {
				warn(fmt.Sprintf("%v %s %s not of length %v", r, field, showOne(data), acceptableLengths))
			}
		}
	}

	switch expected {
	case "bool":
		_, ok := data.(bool)
		if !ok {
			warn(fmt.Sprintf("%v %s not bool", r, field))
		}
	case "int":
		switch data.(type) {
		case int, int64:
			// ok
		default:
			warn(fmt.Sprintf("%v %s not int-like", r, field))
		}
	case "str":
		_, ok := data.(string)
		if !ok {
			warn(fmt.Sprintf("%v %s not string", r, field))
		}
	case "bytes":
		b, ok := data.([]byte)
		if !ok {
			warn(fmt.Sprintf("%v %s not []byte", r, field))
		} else {
			checkLen(b)
		}
	default:
		// Not implemented checks
	}
}

func (r *Record) String() string {
	return fmt.Sprintf("Record(%q, %v)", r.name, r.fields)
}

func (r *Record) humanReadable() []string {
	var lines []string
	for field, data := range r.fields {
		// Match logic from Python code
		switch field {
		case "BKGD":
			r.validateType(field, data, "bytes", 12)
			b, _ := data.([]byte)
			backgroundType := string(b[:4])
			switch backgroundType {
			case "DefB":
				lines = append(lines, "Background: Default")
			case "ClrB":
				hexColor := hex.EncodeToString(b[4:10])
				lines = append(lines, fmt.Sprintf("Background: Color #%s", hexColor))
			case "PctB":
				lines = append(lines, "Background: Picture, see \"Picture\" field")
			default:
				warn("Unrecognized background type " + backgroundType)
				lines = append(lines, fmt.Sprintf("Background (unrecognized): %s", showOne(data)))
			}
		case "GRP0":
			r.validateType(field, data, "str")
			lines = append(lines, fmt.Sprintf("%s (unknown): %v", field, data))
		case "ICVO":
			r.validateType(field, data, "bool")
			lines = append(lines, fmt.Sprintf("%s (unknown): %v", field, data))
		case "Iloc":
			r.validateType(field, data, "bytes", 16)
			b := data.([]byte)
			x := int(binary.BigEndian.Uint32(b[0:4]))
			y := int(binary.BigEndian.Uint32(b[4:8]))
			rest := b[8:16]
			lines = append(lines, fmt.Sprintf("Icon location: x %dpx, y %dpx, %s", x, y, showOne(rest)))
		case "LSVO":
			r.validateType(field, data, "bool")
			lines = append(lines, fmt.Sprintf("%s (unknown): %v", field, data))
		case "bwsp":
			r.validateType(field, data, "bytes")
			val := parsePlist(data.([]byte))
			lines = append(lines, "Layout property list:")
			for _, l := range show(val, 1) {
				lines = append(lines, l)
			}
		case "cmmt":
			r.validateType(field, data, "str")
			lines = append(lines, fmt.Sprintf("Comments: %v", data))
		case "dilc":
			r.validateType(field, data, "bytes", 32)
			b := data.([]byte)
			x := float64(int32(binary.BigEndian.Uint32(b[16:20]))) / 1000.0
			y := float64(int32(binary.BigEndian.Uint32(b[20:24]))) / 1000.0
			before := b[0:16]
			after := b[24:32]
			lines = append(lines, fmt.Sprintf("Icon location on desktop: x %.3f%%, y %.3f%%, %s, %s",
				x, y, showOne(before), showOne(after)))
		case "dscl":
			r.validateType(field, data, "bool")
			lines = append(lines, fmt.Sprintf("Open in list view: %v", data))
		case "extn":
			r.validateType(field, data, "str")
			lines = append(lines, fmt.Sprintf("Extension: %v", data))
		case "fwi0":
			r.validateType(field, data, "bytes", 16)
			b := data.([]byte)
			top := int16(binary.BigEndian.Uint16(b[0:2]))
			left := int16(binary.BigEndian.Uint16(b[2:4]))
			bottom := int16(binary.BigEndian.Uint16(b[4:6]))
			right := int16(binary.BigEndian.Uint16(b[6:8]))
			lines = append(lines, "Finder window information:")
			lines = append(lines, fmt.Sprintf("\tWindow rectangle: top %d, left %d, bottom %d, right %d",
				top, left, bottom, right))
			views := map[string]string{
				"icnv": "Icon view",
				"clmv": "Column view",
				"Nlsv": "List view",
				"Flwv": "Coverflow view",
			}
			viewRaw := string(b[8:12])
			view, ok := views[viewRaw]
			if !ok {
				view = "(unrecognized) " + viewRaw
			}
			lines = append(lines, fmt.Sprintf("View style (might be overtaken): %s", view))
			lines = append(lines, showOne(b[12:16]))
		case "fwsw":
			r.validateType(field, data, "int")
			lines = append(lines, fmt.Sprintf("Finder window sidebar width: %v", data))
		case "fwvh":
			r.validateType(field, data, "int")
			lines = append(lines, fmt.Sprintf("Finder window vertical height (overrides Finder window information): %v", data))
		case "icgo":
			r.validateType(field, data, "bytes", 8)
			lines = append(lines, fmt.Sprintf("%s (unknown): %s", field, showOne(data)))
		case "icsp":
			r.validateType(field, data, "bytes", 8)
			lines = append(lines, fmt.Sprintf("%s (unknown): %s", field, showOne(data)))
		case "icvo":
			r.validateType(field, data, "bytes")
			b := data.([]byte)
			lines = append(lines, "Icon view options:")
			icvoType := string(b[0:4])
			arranges := map[string]string{"none": "None", "grid": "Snap to Grid"}
			labels := map[string]string{"botm": "Bottom", "rght": "Right"}
			switch icvoType {
			case "icvo":
				if len(b) == 18 {
					flags := b[4:12]
					size := int(int16(binary.BigEndian.Uint16(b[12:14])))
					arrangeRaw := string(b[14:18])
					arrange := arranges[arrangeRaw]
					if arrange == "" {
						arrange = "(unknown) " + arrangeRaw
					}
					lines = append(lines, fmt.Sprintf("\tFlags (?): %s", showOne(flags)))
					lines = append(lines, fmt.Sprintf("\tSize: %dpx", size))
					lines = append(lines, fmt.Sprintf("\tKeep arranged by: %s", arrange))
				} else {
					warn("icvo data not length 18")
					lines = append(lines, "\t(unrecognized icvo)")
				}
			case "icv4":
				if len(b) == 26 {
					size := int(int16(binary.BigEndian.Uint16(b[4:6])))
					arrangeRaw := string(b[6:10])
					arrange := arranges[arrangeRaw]
					if arrange == "" {
						arrange = "(unknown) " + arrangeRaw
					}
					labelRaw := string(b[10:14])
					label := labels[labelRaw]
					if label == "" {
						label = "(unknown) " + labelRaw
					}
					flags := b[14:26]
					info := (flags[1] & 0x01) != 0
					preview := (flags[11] & 0x01) != 0
					lines = append(lines, fmt.Sprintf("\tSize: %dpx", size))
					lines = append(lines, fmt.Sprintf("\tKeep arranged by: %s", arrange))
					lines = append(lines, fmt.Sprintf("\tLabel position: %s", label))
					lines = append(lines, "\tFlags (partially known):")
					lines = append(lines, fmt.Sprintf("\t\tRaw flags: %s", showOne(flags)))
					lines = append(lines, fmt.Sprintf("\t\tShow item info: %v", info))
					lines = append(lines, fmt.Sprintf("\t\tShow icon preview: %v", preview))
				} else {
					warn("icv4 data not length 26")
					lines = append(lines, "\t(unrecognized icv4)")
				}
			default:
				warn("Unrecognized icon view options type " + icvoType)
				lines = append(lines, "\t(unrecognized): "+showOne(data))
			}
		case "icvp":
			r.validateType(field, data, "bytes")
			val := parsePlist(data.([]byte))
			lines = append(lines, "Icon view property list:")
			for _, l := range show(val, 1) {
				lines = append(lines, l)
			}
		case "info":
			r.validateType(field, data, "bytes")
			lines = append(lines, fmt.Sprintf("%s (unknown): %s", field, showOne(data)))
		case "logS", "lg1S":
			r.validateType(field, data, "int")
			lines = append(lines, fmt.Sprintf("Logical size: %vB", data))
		case "lssp":
			r.validateType(field, data, "bytes", 8)
			lines = append(lines, fmt.Sprintf("%s (unknown, List view scroll position?): %s", field, showOne(data)))
		case "lsvC":
			r.validateType(field, data, "bytes")
			val := parsePlist(data.([]byte))
			lines = append(lines, "List view properties, alternative:")
			for _, l := range show(val, 1) {
				lines = append(lines, l)
			}
		case "lsvP":
			r.validateType(field, data, "bytes")
			val := parsePlist(data.([]byte))
			lines = append(lines, "List view properties, other alternative:")
			for _, l := range show(val, 1) {
				lines = append(lines, l)
			}
		case "lsvo":
			r.validateType(field, data, "bytes", 76)
			lines = append(lines, fmt.Sprintf("List view options (format unknown): %s", showOne(data)))
		case "lsvp":
			r.validateType(field, data, "bytes")
			val := parsePlist(data.([]byte))
			lines = append(lines, "List view properties:")
			for _, l := range show(val, 1) {
				lines = append(lines, l)
			}
		case "lsvt":
			r.validateType(field, data, "int")
			lines = append(lines, fmt.Sprintf("List view text size: %vpt", data))
		case "moDD", "modD":
			// moDD and modD may be int or bytes
			switch vv := data.(type) {
			case int, int64:
				// Date is number of 1/65536 seconds from 1904
				var date float64
				switch vi := vv.(type) {
				case int:
					date = float64(vi) / 65536.0
				case int64:
					date = float64(vi) / 65536.0
				}
				if field == "moDD" {
					lines = append(lines, fmt.Sprintf("Modification date: %s", showDate(date)))
				} else {
					lines = append(lines, fmt.Sprintf("Modification date, alternative: %s", showDate(date)))
				}
			case []byte:
				// Little endian for some reason
				b := vv
				var date uint64
				switch len(b) {
				case 2:
					date = uint64(binary.LittleEndian.Uint16(b))
				case 4:
					date = uint64(binary.LittleEndian.Uint32(b))
				case 8:
					date = binary.LittleEndian.Uint64(b)
				default:
					// Just parse what we can
					if len(b) <= 8 {
						padded := make([]byte, 8)
						copy(padded, b)
						date = binary.LittleEndian.Uint64(padded)
					} else {
						// too long, just show raw
						lines = append(lines, fmt.Sprintf("Modification date (timestamp, unknown): %s", hex.EncodeToString(b)))
						break
					}
				}
				if field == "moDD" {
					lines = append(lines, fmt.Sprintf("Modification date (timestamp, format unknown): %d", date))
				} else {
					lines = append(lines, fmt.Sprintf("Modification date, alternative (timestamp, format unknown): %d", date))
				}
			}
		case "ph1S", "phyS":
			r.validateType(field, data, "int")
			lines = append(lines, fmt.Sprintf("Physical size: %vB", data))
		case "pict":
			// pict with BKGD
			lines = append(lines, fmt.Sprintf("Picture: %s", showOne(data)))
		case "vSrn":
			r.validateType(field, data, "int")
			lines = append(lines, fmt.Sprintf("%s (unknown): %v", field, data))
		case "vstl":
			r.validateType(field, data, "str")
			views := map[string]string{
				"icnv": "Icon view",
				"clmv": "Column view",
				"glyv": "Gallery view",
				"Nlsv": "List view",
				"Flwv": "Coverflow view",
			}
			strdata := data.(string)
			view, ok := views[strdata]
			if !ok {
				view = "(unrecognized) " + strdata
			}
			lines = append(lines, fmt.Sprintf("View style: %s", view))
		default:
			lines = append(lines, fmt.Sprintf("%s (unrecognized): %v", field, data))
		}
	}
	return lines
}

// parsePlist attempts to parse a plist from a byte slice.
func parsePlist(data []byte) interface{} {
	decoder := plist.NewDecoder(bytes.NewReader(data))
	var val interface{}
	if err := decoder.Decode(&val); err != nil {
		// return raw if fail
		return data
	}
	return val
}

// DSStore struct
type DSStore struct {
	content          []byte
	cursor           int
	records          []*Record
	offsets          []uint32
	allocatorOffset  uint32
	allocatorLength  uint32
	directory        map[string]uint32
	masterID         uint32
	freelist         map[uint32][]uint32
	rootID           uint32
	treeHeight       uint32
	numRecords       uint32
	numNodes         uint32
}

func NewDSStore(content []byte) *DSStore {
	return &DSStore{
		content:  content,
		records:  make([]*Record, 0),
		directory: make(map[string]uint32),
		freelist:  make(map[uint32][]uint32),
	}
}

func (d *DSStore) readRecords() []*Record {
	return d.records
}

// read helpers
func (d *DSStore) nextByte() byte {
	b := d.content[d.cursor]
	d.cursor++
	return b
}

func (d *DSStore) nextBytes(n int) []byte {
	b := d.content[d.cursor : d.cursor+n]
	d.cursor += n
	return b
}

func (d *DSStore) nextUint32() uint32 {
	b := d.nextBytes(4)
	return binary.BigEndian.Uint32(b)
}

func (d *DSStore) nextUint64() uint64 {
	b := d.nextBytes(8)
	return binary.BigEndian.Uint64(b)
}

func (d *DSStore) parseHeader() {
	alignment := d.nextUint32()
	if alignment != 0x00000001 {
		warn(fmt.Sprintf("Alignment int %x not 0x00000001", alignment))
	}
	magic := d.nextUint32()
	if magic != 0x42756431 {
		warn(fmt.Sprintf("Magic bytes %x not 0x42756431 (Bud1)", magic))
	}
	d.allocatorOffset = 0x4 + d.nextUint32()
	d.allocatorLength = d.nextUint32()
	allocatorOffsetRepeat := 0x4 + d.nextUint32()
	if allocatorOffsetRepeat != d.allocatorOffset {
		warn(fmt.Sprintf("Allocator offsets %x and %x unequal", d.allocatorOffset, allocatorOffsetRepeat))
	}
}

func (d *DSStore) parseAllocator() {
	d.cursor = int(d.allocatorOffset)
	numOffsets := d.nextUint32()
	second := d.nextUint32()
	if second != 0 {
		warn(fmt.Sprintf("Second int of allocator %x not 0x00000000", second))
	}
	d.offsets = make([]uint32, numOffsets)
	for i := 0; i < int(numOffsets); i++ {
		d.offsets[i] = d.nextUint32()
	}

	d.cursor = int(d.allocatorOffset) + 0x408
	numKeys := d.nextUint32()
	for i := 0; i < int(numKeys); i++ {
		keyLength := int(d.nextByte())
		keyBytes := d.nextBytes(keyLength)
		key := string(keyBytes)
		val := d.nextUint32()
		d.directory[key] = val
		if key != "DSDB" {
			warn(fmt.Sprintf("Directory contains non-'DSDB' key %q and value %x", key, val))
		}
	}
	dsdbVal, ok := d.directory["DSDB"]
	if !ok {
		panic("Key 'DSDB' not found in table of contents")
	}
	d.masterID = dsdbVal

	for i := 0; i < 32; i++ {
		valuesLength := d.nextUint32()
		list := make([]uint32, valuesLength)
		for j := 0; j < int(valuesLength); j++ {
			list[j] = d.nextUint32()
		}
		d.freelist[1<<i] = list
	}
}

func (d *DSStore) parseTreeNode(nodeID uint32, master bool) {
	offsetAndSize := d.offsets[nodeID]
	d.cursor = 0x4 + int((offsetAndSize>>5)<<5)
	// node size = 1 << (offsetAndSize & 0x1f) but we might not strictly need it

	if master {
		d.rootID = d.nextUint32()
		d.treeHeight = d.nextUint32()
		d.numRecords = d.nextUint32()
		d.numNodes = d.nextUint32()
		fifth := d.nextUint32()
		if fifth != 0x00001000 {
			warn(fmt.Sprintf("Fifth int of master %x not 0x00001000", fifth))
		}
		d.parseTreeNode(d.rootID, false)
	} else {
		nextID := d.nextUint32()
		numRecords := d.nextUint32()
		for i := 0; i < int(numRecords); i++ {
			if nextID != 0 {
				// Has children
				childID := d.nextUint32()
				currentCursor := d.cursor
				d.parseTreeNode(childID, false)
				d.cursor = currentCursor
			}
			nameLength := d.nextUint32()
			nameBytes := d.nextBytes(int(nameLength) * 2)
			name := utf16ToString(nameBytes)
			field := string(d.nextBytes(4))
			dt := d.parseData()

			// Update or create record
			found := false
			for _, rec := range d.records {
				if rec.name == name {
					rec.update(map[string]interface{}{field: dt})
					found = true
					break
				}
			}
			if !found {
				rec := NewRecord(name)
				rec.update(map[string]interface{}{field: dt})
				d.records = append(d.records, rec)
			}
		}
		if nextID != 0 {
			d.parseTreeNode(nextID, false)
		}
	}
}

func (d *DSStore) parseData() interface{} {
	dataType := string(d.nextBytes(4))
	switch dataType {
	case "bool":
		b := d.nextByte()
		return (b & 0x01) != 0
	case "shor", "long":
		// short also uses 4 bytes
		val := d.nextUint32()
		return int(val)
	case "comp":
		val := d.nextUint64()
		return int64(val)
	case "dutc":
		// dutc is int 64
		val := d.nextUint64()
		return int64(val)
	case "type":
		tp := d.nextBytes(4)
		return string(tp)
	case "blob":
		dataLength := d.nextUint32()
		return d.nextBytes(int(dataLength))
	case "ustr":
		dataLength := d.nextUint32()
		bytesData := d.nextBytes(int(dataLength * 2))
		return utf16ToString(bytesData)
	default:
		log.Fatalf("Unrecognized data type %q", dataType)
		return nil
	}
}

func (d *DSStore) Parse() error {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "Error parsing DS_Store:", r)
		}
	}()
	d.parseHeader()
	d.parseAllocator()
	d.parseTreeNode(d.masterID, true)
	return nil
}

func utf16ToString(b []byte) string {
	if len(b)%2 != 0 {
		return ""
	}
	u := make([]uint16, len(b)/2)
	for i := 0; i < len(b); i += 2 {
		u[i/2] = binary.BigEndian.Uint16(b[i : i+2])
	}
	runes := utf16.Decode(u)
	return string(runes)
}

func main() {
	args := os.Args
	filename := ".DS_Store"
	if len(args) == 2 {
		filename = args[1]
	} else if len(args) > 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <.DS_Store file>\n", args[0])
		os.Exit(1)
	} else {
		fmt.Fprintf(os.Stderr, "File unspecified. Using .DS_Store in the current directory...\n")
	}
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}
	ds := NewDSStore(content)
	if err := ds.Parse(); err != nil {
		log.Fatal(err)
	}
	for _, record := range ds.readRecords() {
		fmt.Println(record.name)
		for _, line := range record.humanReadable() {
			fmt.Printf("\t%s\n", line)
		}
	}
}
