package installer

import (
	_ "embed"
	"os"
	"path/filepath"
)

// composerPharBundled is shipped with the binary so Composer is available
// out of the box, without anyone having to click Install on the Tools page
// or having a network connection on first launch. Bumping the bundled
// version is a matter of replacing embedded/composer.phar and updating
// composerBundledVersion to match.
//
//go:embed embedded/composer.phar
var composerPharBundled []byte

const composerBundledVersion = "2.8.5"

// EnsureBundledComposer materializes the embedded composer.phar into
// installTo (typically paths.ComposerRoot()) along with the .bat / shell
// wrappers, IF nothing is installed there yet. Idempotent: a no-op when
// composer.phar already exists in installTo, so an explicit Install from
// the catalog (e.g. a user picking a different version) is preserved.
func EnsureBundledComposer(installTo string) error {
	pharPath := filepath.Join(installTo, "composer.phar")
	if _, err := os.Stat(pharPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(installTo, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(pharPath, composerPharBundled, 0755); err != nil {
		return err
	}
	if err := writeComposerWrappers(installTo); err != nil {
		return err
	}
	return writeMarker(installTo, composerBundledVersion)
}
