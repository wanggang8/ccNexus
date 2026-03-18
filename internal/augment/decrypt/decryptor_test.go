package decrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"
)

// generateTestKeyPEM generates a fresh RSA-2048 key pair for testing.
func generateTestKeyPEM(t *testing.T) (privPEM []byte, pubKey *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	privPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	return privPEM, &key.PublicKey
}

// encryptPayload encrypts plaintext using AES-256-CBC with a random key+IV,
// then encrypts the key+IV with RSA-OAEP(SHA-256), matching the Augment wire format.
func encryptPayload(t *testing.T, pub *rsa.PublicKey, plaintext []byte) (encryptedData, iv string) {
	t.Helper()

	// Generate random AES key (32 bytes) and IV (16 bytes).
	aesKey := make([]byte, 32)
	aesIV := make([]byte, 16)
	rand.Read(aesKey)
	rand.Read(aesIV)

	// AES-256-CBC encrypt.
	block, _ := aes.NewCipher(aesKey)
	padded := pkcs7Pad(plaintext, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, aesIV).CryptBlocks(ciphertext, padded)

	// RSA-OAEP encrypt [aesKey || aesIV].
	keyIV := append(aesKey, aesIV...)
	encKeyIV, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, keyIV, nil)
	if err != nil {
		t.Fatalf("RSA encrypt key+IV: %v", err)
	}

	return base64.StdEncoding.EncodeToString(ciphertext), base64.StdEncoding.EncodeToString(encKeyIV)
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padded := make([]byte, len(data)+padding)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	return padded
}

// --- IsEncrypted ---

func TestIsEncrypted_True(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"encrypted_data": "abc123",
		"iv":             "xyz",
	})
	if !IsEncrypted(body) {
		t.Error("expected IsEncrypted=true for body with encrypted_data+iv")
	}
}

func TestIsEncrypted_False_NoFields(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"message": "hello",
	})
	if IsEncrypted(body) {
		t.Error("expected IsEncrypted=false for plain message body")
	}
}

func TestIsEncrypted_False_PartialFields(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"encrypted_data": "abc123",
	})
	if IsEncrypted(body) {
		t.Error("expected IsEncrypted=false when iv is missing")
	}
}

// --- Decrypt ---

func TestDecrypt_RoundTrip(t *testing.T) {
	privPEM, pubKey := generateTestKeyPEM(t)

	plaintext := `{"model":"claude-3-5-sonnet","message":"hello"}`
	encData, iv := encryptPayload(t, pubKey, []byte(plaintext))

	body, _ := json.Marshal(map[string]interface{}{
		"encrypted_data": encData,
		"iv":             iv,
	})

	dec, err := NewFromPEM(privPEM)
	if err != nil {
		t.Fatalf("NewFromPEM: %v", err)
	}

	result, err := dec.Decrypt(body)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if string(result) != plaintext {
		t.Errorf("expected %q, got %q", plaintext, string(result))
	}
}

func TestDecrypt_InvalidIV(t *testing.T) {
	privPEM, _ := generateTestKeyPEM(t)
	dec, _ := NewFromPEM(privPEM)

	body, _ := json.Marshal(map[string]interface{}{
		"encrypted_data": "deadbeef",
		"iv":             "notbase64!!!",
	})
	_, err := dec.Decrypt(body)
	if err == nil {
		t.Error("expected error for invalid IV")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	_, pubKey := generateTestKeyPEM(t)
	wrongPrivPEM, _ := generateTestKeyPEM(t)

	encData, iv := encryptPayload(t, pubKey, []byte(`{"message":"hi"}`))
	body, _ := json.Marshal(map[string]interface{}{
		"encrypted_data": encData,
		"iv":             iv,
	})

	dec, _ := NewFromPEM(wrongPrivPEM)
	_, err := dec.Decrypt(body)
	if err == nil {
		t.Error("expected error when decrypting with wrong private key")
	}
}

// --- NewFromPEM ---

func TestNewFromPEM_InvalidPEM(t *testing.T) {
	_, err := NewFromPEM([]byte("not a pem"))
	if err == nil {
		t.Error("expected error for invalid PEM data")
	}
}

// --- ReconstructFromPlaintext ---

func TestReconstructFromPlaintext_WithData(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": "claude-3-5-sonnet",
		"data":  "hello world",
	})
	result, err := ReconstructFromPlaintext(body)
	if err != nil {
		t.Fatalf("ReconstructFromPlaintext: %v", err)
	}

	var req map[string]interface{}
	json.Unmarshal(result, &req)
	if req["message"] != "hello world" {
		t.Errorf("expected message='hello world', got %v", req["message"])
	}
	if req["model"] != "claude-3-5-sonnet" {
		t.Errorf("expected model='claude-3-5-sonnet', got %v", req["model"])
	}
}

func TestReconstructFromPlaintext_DefaultModel(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"data": "test",
	})
	result, err := ReconstructFromPlaintext(body)
	if err != nil {
		t.Fatalf("ReconstructFromPlaintext: %v", err)
	}

	var req map[string]interface{}
	json.Unmarshal(result, &req)
	if req["model"] == "" || req["model"] == nil {
		t.Error("expected default model to be set")
	}
}

func TestReconstructFromPlaintext_NoData(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": "claude-3-5-sonnet",
	})
	_, err := ReconstructFromPlaintext(body)
	if err == nil {
		t.Error("expected error when data field is missing")
	}
}

// --- internal helpers ---

func TestPkcs7Unpad_Valid(t *testing.T) {
	// 15 bytes of data + 1 byte pad (0x01)
	data := make([]byte, 16)
	data[15] = 0x01
	result, err := pkcs7Unpad(data)
	if err != nil {
		t.Fatalf("pkcs7Unpad: %v", err)
	}
	if len(result) != 15 {
		t.Errorf("expected 15 bytes, got %d", len(result))
	}
}

func TestPkcs7Unpad_InvalidPadding(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x05} // pad says 5 but only 1 byte would be valid
	_, err := pkcs7Unpad(data)
	if err == nil {
		t.Error("expected error for invalid padding")
	}
}

func TestDecodeHexOrBase64_Hex(t *testing.T) {
	result, err := decodeHexOrBase64("48656c6c6f")
	if err != nil {
		t.Fatalf("decodeHexOrBase64: %v", err)
	}
	if string(result) != "Hello" {
		t.Errorf("expected 'Hello', got %q", string(result))
	}
}

func TestDecodeHexOrBase64_Base64(t *testing.T) {
	result, err := decodeHexOrBase64("SGVsbG8=")
	if err != nil {
		t.Fatalf("decodeHexOrBase64: %v", err)
	}
	if string(result) != "Hello" {
		t.Errorf("expected 'Hello', got %q", string(result))
	}
}
