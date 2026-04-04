package extension

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PackageScope controls whether a package is installed globally or per-project.
type PackageScope string

const (
	PackageScopeGlobal  PackageScope = "global"
	PackageScopeProject PackageScope = "project"
)

// InstalledPackage describes an installed shareable resource package.
type InstalledPackage struct {
	Name        string       `json:"name"`
	Scope       PackageScope `json:"scope"`
	Dir         string       `json:"dir"`
	Source      string       `json:"source,omitempty"`
	SourceType  string       `json:"source_type,omitempty"`
	InstalledAt time.Time    `json:"installed_at,omitempty"`
	UpdatedAt   time.Time    `json:"updated_at,omitempty"`
}

const packageMetaFile = ".pi-package.json"

// InstallPackage installs a resource package into the requested scope.
func InstallPackage(workDir string, scope PackageScope, source, name string) (InstalledPackage, error) {
	if strings.TrimSpace(source) == "" {
		return InstalledPackage{}, fmt.Errorf("package source is required")
	}
	root, err := packageInstallRoot(workDir, scope)
	if err != nil {
		return InstalledPackage{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return InstalledPackage{}, fmt.Errorf("creating package root %s: %w", root, err)
	}

	resolvedSource, sourceType, err := normalizePackageSource(source)
	if err != nil {
		return InstalledPackage{}, err
	}
	if name == "" {
		name = inferPackageName(resolvedSource)
	}
	if name == "" {
		return InstalledPackage{}, fmt.Errorf("could not determine package name from %q", source)
	}
	if err := validatePackageName(name); err != nil {
		return InstalledPackage{}, err
	}

	dest, err := safePackageDir(root, name)
	if err != nil {
		return InstalledPackage{}, err
	}
	if _, err := os.Stat(dest); err == nil {
		return InstalledPackage{}, fmt.Errorf("package %q already installed in %s", name, scope)
	} else if !os.IsNotExist(err) {
		return InstalledPackage{}, fmt.Errorf("checking package dir %s: %w", dest, err)
	}

	record := InstalledPackage{
		Name:        name,
		Scope:       scope,
		Dir:         dest,
		Source:      resolvedSource,
		SourceType:  sourceType,
		InstalledAt: time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := syncPackageSource(record); err != nil {
		return InstalledPackage{}, err
	}
	return record, nil
}

// UpdatePackage refreshes an installed package from its recorded source.
func UpdatePackage(workDir string, scope PackageScope, name string) (InstalledPackage, error) {
	pkg, err := readInstalledPackage(workDir, scope, name)
	if err != nil {
		return InstalledPackage{}, err
	}
	pkg.UpdatedAt = time.Now().UTC()
	if err := syncPackageSource(pkg); err != nil {
		return InstalledPackage{}, err
	}
	return pkg, nil
}

// RemovePackage deletes an installed package directory.
func RemovePackage(workDir string, scope PackageScope, name string) error {
	root, err := packageInstallRoot(workDir, scope)
	if err != nil {
		return err
	}
	if err := validatePackageName(name); err != nil {
		return err
	}
	dest, err := safePackageDir(root, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dest); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("package %q not installed in %s", name, scope)
		}
		return fmt.Errorf("checking package dir %s: %w", dest, err)
	}
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("removing package %q: %w", name, err)
	}
	return nil
}

// ListInstalledPackages returns installed packages from both scopes.
func ListInstalledPackages(workDir string) ([]InstalledPackage, error) {
	var pkgs []InstalledPackage
	for _, scope := range []PackageScope{PackageScopeGlobal, PackageScopeProject} {
		root, err := packageInstallRoot(workDir, scope)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading package root %s: %w", root, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			pkg, err := readInstalledPackage(workDir, scope, entry.Name())
			if err != nil {
				return nil, err
			}
			pkgs = append(pkgs, pkg)
		}
	}
	sort.Slice(pkgs, func(i, j int) bool {
		if pkgs[i].Name == pkgs[j].Name {
			return pkgs[i].Scope < pkgs[j].Scope
		}
		return pkgs[i].Name < pkgs[j].Name
	})
	return pkgs, nil
}

func packageInstallRoot(workDir string, scope PackageScope) (string, error) {
	switch scope {
	case PackageScopeGlobal:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home dir: %w", err)
		}
		return filepath.Join(home, ".pi-go", "packages"), nil
	case PackageScopeProject:
		if strings.TrimSpace(workDir) == "" {
			return "", fmt.Errorf("project package operations require a working directory")
		}
		return filepath.Join(workDir, ".pi-go", "packages"), nil
	default:
		return "", fmt.Errorf("unknown package scope %q", scope)
	}
}

func readInstalledPackage(workDir string, scope PackageScope, name string) (InstalledPackage, error) {
	root, err := packageInstallRoot(workDir, scope)
	if err != nil {
		return InstalledPackage{}, err
	}
	if err := validatePackageName(name); err != nil {
		return InstalledPackage{}, err
	}
	dir, err := safePackageDir(root, name)
	if err != nil {
		return InstalledPackage{}, err
	}
	metaPath := filepath.Join(dir, packageMetaFile)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return InstalledPackage{Name: name, Scope: scope, Dir: dir}, nil
		}
		return InstalledPackage{}, fmt.Errorf("reading package metadata %s: %w", metaPath, err)
	}
	var pkg InstalledPackage
	if err := json.Unmarshal(data, &pkg); err != nil {
		return InstalledPackage{}, fmt.Errorf("parsing package metadata %s: %w", metaPath, err)
	}
	pkg.Name = name
	pkg.Scope = scope
	pkg.Dir = dir
	return pkg, nil
}

func syncPackageSource(pkg InstalledPackage) error {
	parent := filepath.Dir(pkg.Dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("creating package parent dir %s: %w", parent, err)
	}
	tempDir, err := os.MkdirTemp(parent, pkg.Name+"-tmp-")
	if err != nil {
		return fmt.Errorf("creating temp package dir for %q: %w", pkg.Name, err)
	}
	defer func() {
		if _, err := os.Stat(tempDir); err == nil {
			_ = os.RemoveAll(tempDir)
		}
	}()

	tempPkg := pkg
	tempPkg.Dir = tempDir
	switch pkg.SourceType {
	case "local":
		if err := copyDir(pkg.Source, tempDir, func(path string, d fs.DirEntry) bool {
			return d.Name() == ".git"
		}); err != nil {
			return fmt.Errorf("copying package %q from %s: %w", pkg.Name, pkg.Source, err)
		}
	case "git":
		if err := os.RemoveAll(tempDir); err != nil {
			return fmt.Errorf("resetting temp package dir %s: %w", tempDir, err)
		}
		cmd := exec.Command("git", "clone", "--depth", "1", pkg.Source, tempDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("cloning package %q from %s: %w: %s", pkg.Name, pkg.Source, err, strings.TrimSpace(string(out)))
		}
	default:
		return fmt.Errorf("unsupported package source type %q", pkg.SourceType)
	}
	if err := writePackageMetadata(tempPkg); err != nil {
		return err
	}
	return replaceDirAtomic(tempDir, pkg.Dir)
}

func writePackageMetadata(pkg InstalledPackage) error {
	if err := os.MkdirAll(pkg.Dir, 0o755); err != nil {
		return fmt.Errorf("creating package dir %s: %w", pkg.Dir, err)
	}
	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding package metadata for %q: %w", pkg.Name, err)
	}
	if err := os.WriteFile(filepath.Join(pkg.Dir, packageMetaFile), data, 0o644); err != nil {
		return fmt.Errorf("writing package metadata for %q: %w", pkg.Name, err)
	}
	return nil
}

func normalizePackageSource(source string) (string, string, error) {
	if info, err := os.Stat(source); err == nil {
		if !info.IsDir() {
			return "", "", fmt.Errorf("package source %q is not a directory", source)
		}
		abs, err := filepath.Abs(source)
		if err != nil {
			return "", "", fmt.Errorf("resolving package source %q: %w", source, err)
		}
		return abs, "local", nil
	}
	if looksLikeGitSource(source) {
		return source, "git", nil
	}
	return "", "", fmt.Errorf("package source %q does not exist and is not a supported git URL", source)
}

func looksLikeGitSource(source string) bool {
	return strings.HasPrefix(source, "git@") ||
		strings.HasPrefix(source, "http://") ||
		strings.HasPrefix(source, "https://") ||
		strings.HasPrefix(source, "ssh://")
}

func validatePackageName(name string) error {
	if inferPackageName(name) != name {
		return fmt.Errorf("invalid package name %q: use only lowercase letters, numbers, '.', '_' and '-'", name)
	}
	if name == "." || name == ".." || strings.Contains(name, "..") || strings.ContainsRune(name, filepath.Separator) {
		return fmt.Errorf("invalid package name %q", name)
	}
	return nil
}

func safePackageDir(root, name string) (string, error) {
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving package root %s: %w", root, err)
	}
	dir := filepath.Join(cleanRoot, name)
	rel, err := filepath.Rel(cleanRoot, dir)
	if err != nil {
		return "", fmt.Errorf("resolving package path for %q: %w", name, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("package %q escapes package root", name)
	}
	return dir, nil
}

func inferPackageName(source string) string {
	trimmed := strings.TrimSuffix(filepath.Base(source), ".git")
	trimmed = strings.TrimSpace(trimmed)
	trimmed = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		case r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, trimmed)
	return strings.Trim(trimmed, "-._")
}

func replaceDirAtomic(tempDir, dest string) error {
	backup := dest + ".bak"
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("clearing package backup dir %s: %w", backup, err)
	}
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, backup); err != nil {
			return fmt.Errorf("moving existing package %s aside: %w", dest, err)
		}
	}
	if err := os.Rename(tempDir, dest); err != nil {
		if _, restoreErr := os.Stat(backup); restoreErr == nil {
			_ = os.Rename(backup, dest)
		}
		return fmt.Errorf("activating package dir %s: %w", dest, err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("removing package backup dir %s: %w", backup, err)
	}
	return nil
}

func copyDir(src, dst string, skip func(path string, d fs.DirEntry) bool) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == src {
			return os.MkdirAll(dst, 0o755)
		}
		if skip != nil && skip(path, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := copyFile(path, target); err != nil {
			return err
		}
		return nil
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
