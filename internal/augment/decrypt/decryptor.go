package decrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
)

// Decryptor handles RSA+AES-256-CBC decryption for Augment plugin requests.
// Augment encrypts the request body with AES-256-CBC and encrypts the AES
// key+IV with RSA-OAEP(SHA-256). The encrypted payload structure is:
//
//	{ "encrypted_data": "<hex or base64 AES ciphertext>", "iv": "<base64 RSA-encrypted AES key+IV>" }
type Decryptor struct {
	privateKey *rsa.PrivateKey
}

// New creates a Decryptor by loading the RSA private key from keyPath.
func New(keyPath string) (*Decryptor, error) {
	pemData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("augment decryptor: read key %s: %w", keyPath, err)
	}
	return NewFromPEM(pemData)
}

// NewFromPEM creates a Decryptor from raw PEM bytes (used with go:embed).
func NewFromPEM(pemData []byte) (*Decryptor, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("augment decryptor: invalid PEM data")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("augment decryptor: parse private key: %w", err)
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("augment decryptor: not an RSA private key")
	}

	return &Decryptor{privateKey: rsaKey}, nil
}

// encryptedBody is the wire format for encrypted Augment requests.
type encryptedBody struct {
	EncryptedData string `json:"encrypted_data"`
	IV            string `json:"iv"`
}

// IsEncrypted reports whether body is an encrypted Augment request.
func IsEncrypted(body []byte) bool {
	var eb encryptedBody
	if err := json.Unmarshal(body, &eb); err != nil {
		return false
	}
	return eb.EncryptedData != "" && eb.IV != ""
}

// Decrypt decrypts an encrypted Augment request body and returns the
// plaintext JSON bytes of the original AugmentRequest.
func (d *Decryptor) Decrypt(body []byte) ([]byte, error) {
	var eb encryptedBody
	if err := json.Unmarshal(body, &eb); err != nil {
		return nil, fmt.Errorf("augment decryptor: unmarshal body: %w", err)
	}

	// Step 1: RSA-OAEP decrypt the IV field to obtain AES key+IV.
	ivBytes, err := base64.StdEncoding.DecodeString(eb.IV)
	if err != nil {
		return nil, fmt.Errorf("augment decryptor: decode IV: %w", err)
	}

	decryptedKeyIV, err := rsa.DecryptOAEP(sha256.New(), nil, d.privateKey, ivBytes, nil)
	if err != nil {
		return nil, fmt.Errorf("augment decryptor: RSA decrypt: %w", err)
	}

	// Step 2: Parse AES key and IV from decrypted blob.
	// Two formats:
	//   Binary: 48 bytes — first 32 bytes AES key, last 16 bytes IV
	//   Text:   "keyHex::ivHex"
	aesKey, aesIV, err := parseAESKeyIV(decryptedKeyIV)
	if err != nil {
		return nil, fmt.Errorf("augment decryptor: parse AES key/IV: %w", err)
	}

	// Step 3: AES-256-CBC decrypt the payload.
	// encrypted_data may be hex or base64 encoded.
	ciphertext, err := decodeHexOrBase64(eb.EncryptedData)
	if err != nil {
		return nil, fmt.Errorf("augment decryptor: decode ciphertext: %w", err)
	}

	plaintext, err := aesCBCDecrypt(aesKey, aesIV, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("augment decryptor: AES decrypt: %w", err)
	}

	return plaintext, nil
}

// ReconstructFromPlaintext extracts a minimal AugmentRequest from the plaintext
// "data" field when decryption fails or the request is not encrypted.
func ReconstructFromPlaintext(body []byte) ([]byte, error) {
	var raw struct {
		Model  string   `json:"model"`
		Data   string   `json:"data"`
		Images []string `json:"images"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("augment decryptor: unmarshal plaintext: %w", err)
	}
	if raw.Data == "" {
		return nil, fmt.Errorf("augment decryptor: no data field in plaintext body")
	}

	model := raw.Model
	if model == "" {
		model = "claude-sonnet-4-5-20250929"
	}
	streamTrue := true
	result := map[string]interface{}{
		"model":      model,
		"message":    raw.Data,
		"images":     raw.Images,
		"max_tokens": 4096,
		"stream":     streamTrue,
	}
	return json.Marshal(result)
}

// parseAESKeyIV extracts a 32-byte AES key and 16-byte IV from the RSA-decrypted blob.
func parseAESKeyIV(decrypted []byte) (key, iv []byte, err error) {
	if len(decrypted) == 48 {
		// Binary format: [32-byte key][16-byte IV]
		return decrypted[:32], decrypted[32:48], nil
	}

	// Text format: "keyHex::ivHex"
	parts := strings.SplitN(string(decrypted), "::", 2)
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("unexpected decrypted blob length %d and not key::iv format", len(decrypted))
	}

	h := sha256.New()
	h.Write([]byte(parts[0]))
	key = h.Sum(nil)

	iv, err = decodeHexBytes(parts[1])
	if err != nil {
		return nil, nil, fmt.Errorf("decode IV hex: %w", err)
	}

	return key, iv, nil
}

// aesCBCDecrypt decrypts ciphertext using AES-256-CBC with PKCS7 padding.
func aesCBCDecrypt(key, iv, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d is not a multiple of block size", len(ciphertext))
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	// Remove PKCS7 padding
	plaintext, err = pkcs7Unpad(plaintext)
	if err != nil {
		return nil, fmt.Errorf("unpad: %w", err)
	}

	return plaintext, nil
}

// pkcs7Unpad removes PKCS7 padding from plaintext.
func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > aes.BlockSize || padLen > len(data) {
		return nil, fmt.Errorf("invalid padding length %d", padLen)
	}
	for i := len(data) - padLen; i < len(data); i++ {
		if data[i] != byte(padLen) {
			return nil, fmt.Errorf("invalid padding byte at %d", i)
		}
	}
	return data[:len(data)-padLen], nil
}

// decodeHexOrBase64 tries hex decoding first, falls back to base64.
func decodeHexOrBase64(s string) ([]byte, error) {
	b, err := decodeHexBytes(s)
	if err == nil {
		return b, nil
	}
	b, err = base64.StdEncoding.DecodeString(s)
	if err == nil {
		return b, nil
	}
	return nil, fmt.Errorf("cannot decode as hex or base64")
}

// decodeHexBytes decodes a hex string (without stdlib to keep simple).
func decodeHexBytes(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd hex string length")
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		hi := hexVal(s[i])
		lo := hexVal(s[i+1])
		if hi < 0 || lo < 0 {
			return nil, fmt.Errorf("invalid hex char at %d", i)
		}
		b[i/2] = byte(hi<<4 | lo)
	}
	return b, nil
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}
