// Package version gère le versionning M.m.f du projet :
//   - M (major) : bump manuel, utilisateur uniquement (jamais en CI).
//   - m (minor) : bump automatisé à l'ajout de fonctionnalités.
//   - f (fix)   : bump pour tout le reste (défaut en CI).
package version

import (
	"fmt"
	"strconv"
	"strings"
)

// Version représente un numéro M.m.f.
type Version struct {
	Major int
	Minor int
	Fix   int
}

// Parse lit "M.m.f" (préfixe "v" toléré, ex. tags "v1.2.3").
func Parse(s string) (Version, error) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "v"))
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version %q : format M.m.f attendu", s)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("version %q : composant %d invalide", s, i+1)
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Fix: nums[2]}, nil
}

// String rend "M.m.f".
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Fix)
}

// Tag rend le nom de tag git "vM.m.f".
func (v Version) Tag() string {
	return "v" + v.String()
}

// BumpMajor incrémente M et réinitialise m et f.
// Réservé à l'utilisateur, jamais appelé par la CI.
func (v Version) BumpMajor() Version {
	return Version{Major: v.Major + 1}
}

// BumpMinor incrémente m et réinitialise f.
func (v Version) BumpMinor() Version {
	return Version{Major: v.Major, Minor: v.Minor + 1}
}

// BumpFix incrémente f.
func (v Version) BumpFix() Version {
	return Version{Major: v.Major, Minor: v.Minor, Fix: v.Fix + 1}
}
