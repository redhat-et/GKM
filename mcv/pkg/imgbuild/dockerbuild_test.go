package imgbuild

import (
	"encoding/json"
	"testing"

	// Import hash algorithms to register them with crypto package
	_ "crypto/sha256" // Registers SHA256
	_ "crypto/sha512" // Registers SHA384 and SHA512

	"github.com/stretchr/testify/assert"
)

const (
	testDigestSHA256 = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testDigestSHA384 = "sha384:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testDigestSHA512 = "sha512:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestExtractPushedDigest(t *testing.T) {
	const (
		imageRef = "localhost:5000/gkm/cache:test"
		digest   = testDigestSHA256
		wantRef  = "localhost:5000/gkm/cache@" + digest
	)

	tests := []struct {
		name    string
		raw     string
		image   string
		want    string
		wantErr bool
	}{
		{
			name: "docker aux Digest capital D",
			raw: `{
  "status": "Pushing",
  "progressDetail": {},
  "id": "latest"
}
{"status":"latest: digest: ` + digest + ` size: 1234","aux":{"Tag":"latest","Digest":"` + digest + `","Size":1234}}
`,
			image: imageRef,
			want:  wantRef,
		},
		{
			name: "aux digest lowercase key",
			raw: `{"aux":{"tag":"latest","digest":"` + digest + `","size":1234}}
`,
			image: imageRef,
			want:  wantRef,
		},
		{
			name: "podman status-only legacy stream",
			raw: `{"status":"The push refers to repository [localhost:5000/gkm/cache]"}
{"status":"latest: digest: ` + digest + ` size: 1234"}
`,
			image: imageRef,
			want:  wantRef,
		},
		{
			name: "status digest without sha256 prefix is normalized",
			raw: `{"status":"latest: digest: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef size: 99"}
`,
			image: imageRef,
			want:  wantRef,
		},
		{
			name: "ignores aux digest without tag",
			raw: `{"aux":{"Digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
{"status":"Pushing"}
`,
			image: imageRef,
			want:  "",
		},
		{
			name: "prefers manifest aux over earlier layer digest aux",
			raw: `{"status":"Pushing","id":"layer1"}
{"aux":{"Digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
{"status":"latest: digest: ` + digest + ` size: 10","aux":{"Tag":"latest","Digest":"` + digest + `","Size":10}}
`,
			image: imageRef,
			want:  wantRef,
		},
		{
			name: "ignores non-digest aux payloads",
			raw: `{"aux":{"ID":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
{"status":"Pushing"}
`,
			image: imageRef,
			want:  "",
		},
		{
			name:  "empty stream",
			raw:   "",
			image: imageRef,
			want:  "",
		},
		{
			name:    "invalid image ref",
			raw:     `{"aux":{"Tag":"latest","Digest":"` + digest + `"}}`,
			image:   ":::bad",
			wantErr: true,
		},
		{
			name:    "invalid json in stream",
			raw:     "{not-json\n",
			image:   imageRef,
			wantErr: true,
		},
		{
			name: "aux empty Digest falls back to status",
			raw: `{"status":"latest: digest: ` + digest + ` size: 1","aux":{"Tag":"latest","Digest":"","Size":1}}
`,
			image: imageRef,
			want:  wantRef,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractPushedDigest([]byte(tt.raw), tt.image)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeManifestDigest(t *testing.T) {
	// SHA256 tests
	validSHA256 := testDigestSHA256
	assert.Equal(t, validSHA256, normalizeManifestDigest(validSHA256))
	assert.Equal(t, validSHA256, normalizeManifestDigest("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))
	assert.Equal(t, validSHA256, normalizeManifestDigest("SHA256:0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"))

	// SHA384 tests
	validSHA384 := testDigestSHA384
	assert.Equal(t, validSHA384, normalizeManifestDigest(validSHA384))
	assert.Equal(t, validSHA384, normalizeManifestDigest("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))
	assert.Equal(t, validSHA384, normalizeManifestDigest("SHA384:0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"))

	// SHA512 tests
	validSHA512 := testDigestSHA512
	assert.Equal(t, validSHA512, normalizeManifestDigest(validSHA512))
	assert.Equal(t, validSHA512, normalizeManifestDigest("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))
	assert.Equal(t, validSHA512, normalizeManifestDigest("SHA512:0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"))

	// Error cases
	assert.Equal(t, "", normalizeManifestDigest(""))
	assert.Equal(t, "", normalizeManifestDigest("sha256:short"))
	assert.Equal(t, "", normalizeManifestDigest("sha256:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"))
	assert.Equal(t, "", normalizeManifestDigest("md5:0123456789abcdef0123456789abcdef"))
	assert.Equal(t, "", normalizeManifestDigest("sha384:short"))
	assert.Equal(t, "", normalizeManifestDigest("sha512:short"))
	assert.Equal(t, "", normalizeManifestDigest("wronglength123456789abcdef"))
}

func TestDigestFromPushStatus(t *testing.T) {
	d := testDigestSHA256
	assert.Equal(t, d, digestFromPushStatus("latest: digest: "+d+" size: 1234"))
	assert.Equal(t, "", digestFromPushStatus("DIGEST: "+d))
	assert.Equal(t, "", digestFromPushStatus("Pushing [====>] 1B/2B"))
	assert.Equal(t, "", digestFromPushStatus(""))
}

func TestDigestFromManifestPushAux(t *testing.T) {
	d := testDigestSHA256
	assert.Equal(t, d, digestFromManifestPushAux(json.RawMessage(`{"Tag":"latest","Digest":"`+d+`"}`)))
	assert.Equal(t, "", digestFromManifestPushAux(json.RawMessage(`{"Digest":"`+d+`"}`)))
	assert.Equal(t, "", digestFromManifestPushAux(json.RawMessage(`{"Tag":"latest","Digest":""}`)))
}

func TestConfirmPushedDigest_Reconcile(t *testing.T) {
	const digest = testDigestSHA256
	imageRef := "localhost:5000/gkm/cache:test"
	streamRef := "localhost:5000/gkm/cache@" + digest

	got, err := confirmPushedDigest(imageRef, streamRef)
	assert.NoError(t, err)
	assert.Equal(t, streamRef, got, "HEAD unavailable falls back to stream-reported digest")

	got, err = confirmPushedDigest(imageRef, "")
	assert.NoError(t, err)
	assert.Equal(t, "", got)
}
