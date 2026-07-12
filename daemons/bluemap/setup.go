package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// bluemapVersion is the BlueMap CLI release this daemon provisions (override
// with BLUEMAP_VERSION). 5.x requires a Java 25 runtime.
const bluemapVersion = "5.22"

// download fetches url to path via a temp file (no partial artifacts).
func download(url, path string) error {
	log.Printf("downloading %s", url)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ensureBlueMap returns the path to the BlueMap CLI jar, downloading the
// pinned release on first run.
func ensureBlueMap(dataDir, version string) (string, error) {
	jar := filepath.Join(dataDir, "bluemap-"+version+"-cli.jar")
	if _, err := os.Stat(jar); err == nil {
		return jar, nil
	}
	url := fmt.Sprintf("https://github.com/BlueMap-Minecraft/BlueMap/releases/download/v%s/bluemap-%s-cli.jar",
		version, version)
	return jar, download(url, jar)
}

// javaMajor runs `java -version` and reports the major version (0 if it
// cannot be determined).
func javaMajor(java string) int {
	out, err := exec.Command(java, "-version").CombinedOutput()
	if err != nil {
		return 0
	}
	// First line: openjdk version "25.0.1" / java version "21.0.2" ...
	line := strings.SplitN(string(out), "\n", 2)[0]
	i := strings.IndexByte(line, '"')
	if i < 0 {
		return 0
	}
	ver := line[i+1:]
	if j := strings.IndexAny(ver, ".\""); j > 0 {
		ver = ver[:j]
	}
	n, _ := strconv.Atoi(ver)
	return n
}

// ensureJava returns a Java runtime new enough for BlueMap: an explicit
// override first, then PATH, then a previously provisioned JRE, and as a
// last resort a Temurin JRE downloaded into the data dir.
func ensureJava(dataDir, override string, needMajor int) (string, error) {
	if override != "" {
		if javaMajor(override) >= needMajor {
			return override, nil
		}
		return "", fmt.Errorf("%s is not Java %d+", override, needMajor)
	}
	if javaMajor("java") >= needMajor {
		return "java", nil
	}
	local := filepath.Join(dataDir, "jre", "bin", "java")
	if javaMajor(local) >= needMajor {
		return local, nil
	}

	arch := map[string]string{"amd64": "x64", "arm64": "aarch64"}[runtime.GOARCH]
	if arch == "" || runtime.GOOS != "linux" {
		return "", fmt.Errorf("no Java %d+ found and auto-provisioning only supports linux amd64/arm64 (set BLUEMAP_JAVA)", needMajor)
	}
	url := fmt.Sprintf("https://api.adoptium.net/v3/binary/latest/%d/ga/linux/%s/jre/hotspot/normal/eclipse",
		needMajor, arch)
	tgz := filepath.Join(dataDir, "jre.tar.gz")
	if err := download(url, tgz); err != nil {
		return "", err
	}
	if err := untarStripped(tgz, filepath.Join(dataDir, "jre")); err != nil {
		return "", err
	}
	os.Remove(tgz)
	if javaMajor(local) < needMajor {
		return "", fmt.Errorf("provisioned JRE at %s does not run", local)
	}
	return local, nil
}

// untarStripped extracts a .tar.gz into dst, stripping the archive's single
// top-level directory (jdk-25.x+y-jre/...).
func untarStripped(tgz, dst string) error {
	f, err := os.Open(tgz)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		parts := strings.SplitN(filepath.Clean(hdr.Name), string(filepath.Separator), 2)
		if len(parts) < 2 || parts[1] == "" {
			continue // the top-level dir itself
		}
		rel := parts[1]
		if strings.Contains(rel, "..") {
			continue
		}
		path := filepath.Join(dst, rel)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.Remove(path)
			if err := os.Symlink(hdr.Linkname, path); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
				os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
}

// mapDef is one rendered map (one dimension of the exported save).
type mapDef struct {
	id, name, dimension string
	sorting             int
}

var mapDefs = map[string]mapDef{
	"overworld": {"overworld", "Overworld", "minecraft:overworld", 0},
	"nether":    {"nether", "Nether", "minecraft:the_nether", 100},
	"end":       {"end", "End", "minecraft:the_end", 200},
}

// writeConfigs lays down the BlueMap config tree: core (resource download
// consent), webserver (our port), and one map per exported dimension.
// BlueMap generates any config it needs that we don't write.
func writeConfigs(dataDir, savePath string, port int, dims []string) error {
	conf := filepath.Join(dataDir, "config")
	if err := os.MkdirAll(filepath.Join(conf, "maps"), 0o755); err != nil {
		return err
	}
	core := "" +
		"accept-download: true\n" + // consent asserted via -accept-download
		"data: \"data\"\n" +
		"metrics: false\n" +
		"scan-for-mod-resources: false\n"
	if err := os.WriteFile(filepath.Join(conf, "core.conf"), []byte(core), 0o644); err != nil {
		return err
	}
	web := fmt.Sprintf("enabled: true\nwebroot: \"web\"\nip: \"0.0.0.0\"\nport: %d\n", port)
	if err := os.WriteFile(filepath.Join(conf, "webserver.conf"), []byte(web), 0o644); err != nil {
		return err
	}
	for _, d := range dims {
		m, ok := mapDefs[d]
		if !ok {
			return fmt.Errorf("unknown dimension %q", d)
		}
		mc := fmt.Sprintf("world: %q\ndimension: %q\nname: %q\nsorting: %d\n",
			savePath, m.dimension, m.name, m.sorting)
		if err := os.WriteFile(filepath.Join(conf, "maps", m.id+".conf"), []byte(mc), 0o644); err != nil {
			return err
		}
	}
	// Drop map configs for dimensions we no longer export (stale maps
	// otherwise error against a missing region dir).
	entries, _ := os.ReadDir(filepath.Join(conf, "maps"))
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".conf")
		keep := false
		for _, d := range dims {
			if id == d {
				keep = true
			}
		}
		if !keep {
			os.Remove(filepath.Join(conf, "maps", e.Name()))
		}
	}
	return nil
}
