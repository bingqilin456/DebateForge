package debate

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
)

func TestNeonProfileAvatarValidation(t *testing.T) {
	seal, err := normalizeNeonProfile(neonProfileInput{Nickname: "  辩手甲 ", AvatarKind: "seal", AvatarSeal: "弈"})
	if err != nil || seal.Nickname != "辩手甲" || seal.AvatarSeal != "弈" {
		t.Fatalf("seal=%+v err=%v", seal, err)
	}
	input := neonProfileInput{Nickname: "辩手甲", AvatarKind: "image", AvatarImage: "data:image/svg+xml;base64,PHN2Zy8+"}
	if _, err := normalizeNeonProfile(input); err == nil {
		t.Fatal("accepted active image format")
	}
	var photo bytes.Buffer
	pixel := image.NewRGBA(image.Rect(0, 0, 2, 2))
	pixel.Set(0, 0, color.White)
	if err := jpeg.Encode(&photo, pixel, nil); err != nil {
		t.Fatal(err)
	}
	photo.WriteString("untrusted metadata")
	input.AvatarImage = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(photo.Bytes())
	clean, err := normalizeNeonProfile(input)
	if err != nil || !strings.HasPrefix(clean.AvatarImage, "data:image/jpeg;base64,") || clean.AvatarImage == input.AvatarImage {
		t.Fatalf("image normalization failed: %v", err)
	}
}

func TestNeonPublicProfileFields(t *testing.T) {
	row := neonProfileRow{Elo: 1500, Games: 4, Wins: 3, UnlockedIndex: 2}
	profile := row.publicDTO()
	if profile.Nickname != "思弈阁辩手" || profile.AvatarSeal != "辩" || profile.WinRate != 75 || profile.RankLabel != "国赛辩手" || profile.CupStage != "半决赛" {
		t.Fatalf("profile=%+v", profile)
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"username", "password", "games", "wins", "user_id", "vulnerabilities", "elo_history"} {
		if strings.Contains(string(encoded), `"`+forbidden+`"`) {
			t.Fatalf("public profile exposed %q: %s", forbidden, encoded)
		}
	}
}
