package imgbuild

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractPushedDigest(t *testing.T) {
	const (
		imageRef = "localhost:5000/gkm/cache:test"
		digest   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
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
	valid := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	assert.Equal(t, valid, normalizeManifestDigest(valid))
	assert.Equal(t, valid, normalizeManifestDigest("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))
	assert.Equal(t, valid, normalizeManifestDigest("SHA256:0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"))
	assert.Equal(t, "", normalizeManifestDigest(""))
	assert.Equal(t, "", normalizeManifestDigest("sha256:short"))
	assert.Equal(t, "", normalizeManifestDigest("sha256:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"))
	assert.Equal(t, "", normalizeManifestDigest("md5:0123456789abcdef0123456789abcdef"))
}

func TestDigestFromPushStatus(t *testing.T) {
	d := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	assert.Equal(t, d, digestFromPushStatus("latest: digest: "+d+" size: 1234"))
	assert.Equal(t, "", digestFromPushStatus("DIGEST: "+d))
	assert.Equal(t, "", digestFromPushStatus("Pushing [====>] 1B/2B"))
	assert.Equal(t, "", digestFromPushStatus(""))
}

func TestDigestFromManifestPushAux(t *testing.T) {
	d := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	assert.Equal(t, d, digestFromManifestPushAux(json.RawMessage(`{"Tag":"latest","Digest":"`+d+`"}`)))
	assert.Equal(t, "", digestFromManifestPushAux(json.RawMessage(`{"Digest":"`+d+`"}`)))
	assert.Equal(t, "", digestFromManifestPushAux(json.RawMessage(`{"Tag":"latest","Digest":""}`)))
}

func TestConfirmPushedDigest_Reconcile(t *testing.T) {
	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	imageRef := "localhost:5000/gkm/cache:test"
	streamRef := "localhost:5000/gkm/cache@" + digest

	got, err := confirmPushedDigest(imageRef, streamRef)
	assert.NoError(t, err)
	assert.Equal(t, streamRef, got, "HEAD unavailable falls back to stream-reported digest")

	got, err = confirmPushedDigest(imageRef, "")
	assert.NoError(t, err)
	assert.Equal(t, "", got)
}
