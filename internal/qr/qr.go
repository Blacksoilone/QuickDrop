// Package qr renders QR codes as PNG bytes.
package qr

import "github.com/skip2/go-qrcode"

// Render encodes url as a 320x320 medium-EC QR PNG and returns the bytes.
func Render(url string) ([]byte, error) {
	return qrcode.Encode(url, qrcode.Medium, 320)
}
