package ant

import "testing"

func TestStickInfoStringSanitizesControlChars(t *testing.T) {
	tests := []struct {
		name string
		info StickInfo
		want string
	}{
		{
			name: "clean serial",
			info: StickInfo{Serial: "123", Product: "usb2", Bus: 1, Address: 7},
			want: "usb2 serial=123 bus=1 addr=7",
		},
		{
			name: "broken descriptor with control bytes",
			info: StickInfo{Serial: "123\x00?????\x00????\x00DSI\x00\x00", Product: "usb2", Bus: 1, Address: 7},
			want: "usb2 serial=123?????????DSI bus=1 addr=7",
		},
		{
			name: "only control chars falls back to unreadable",
			info: StickInfo{Serial: "\x00\x1b\x7f", Product: "usb2", Bus: 1, Address: 5},
			want: "usb2 serial=<unreadable> bus=1 addr=5",
		},
		{
			name: "empty serial",
			info: StickInfo{Product: "usb2", Bus: 1, Address: 5},
			want: "usb2 serial=<unreadable> bus=1 addr=5",
		},
		{
			name: "esc and ansi attempts stripped",
			info: StickInfo{Serial: "ab\x1b[31mcd", Product: "usb2", Bus: 1, Address: 3},
			want: "usb2 serial=ab[31mcd bus=1 addr=3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.String(); got != tt.want {
				t.Errorf("StickInfo.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeSerial(t *testing.T) {
	if got := SanitizeSerial("a\x00b\x1bc\x7fd"); got != "abcd" {
		t.Errorf("SanitizeSerial = %q, want %q", got, "abcd")
	}
	if got := SanitizeSerial("plain-123"); got != "plain-123" {
		t.Errorf("SanitizeSerial = %q, want unchanged", got)
	}
}
