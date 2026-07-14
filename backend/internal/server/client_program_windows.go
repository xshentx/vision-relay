//go:build windows

package server

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

const appModelPackageRegistryPath = `Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\AppModel\Repository\Packages`

func codexStoreClientProgramCandidates() []string {
	return codexStoreExecutableCandidates(codexStorePackageRoots())
}

func codexStorePackageRoots() []string {
	key, err := registry.OpenKey(registry.CURRENT_USER, appModelPackageRegistryPath, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()

	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	roots := make([]string, 0, 1)
	for _, name := range names {
		if !strings.HasPrefix(strings.ToLower(name), "openai.codex_") {
			continue
		}
		packageKey, openErr := registry.OpenKey(key, name, registry.QUERY_VALUE)
		if openErr != nil {
			continue
		}
		root, _, valueErr := packageKey.GetStringValue("PackageRootFolder")
		packageKey.Close()
		if valueErr == nil && strings.TrimSpace(root) != "" {
			roots = append(roots, root)
		}
	}
	return roots
}
