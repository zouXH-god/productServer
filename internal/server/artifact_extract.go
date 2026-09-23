package server

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var errExtractTooLarge = errors.New("extracted files exceed upload limits")

// expandUploadedZIPs keeps each uploaded ZIP and adds its regular-file entries
// as independently downloadable artifacts in the same staging directory.
func expandUploadedZIPs(staging string, files []ArtifactFile, total, maxFile, maxBatch int64) ([]ArtifactFile, int64, error) {
	uploaded := append([]ArtifactFile(nil), files...)
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		seen[file.OriginalName] = true
	}
	for _, archive := range uploaded {
		if !strings.EqualFold(filepath.Ext(archive.OriginalName), ".zip") {
			continue
		}
		reader, err := zip.OpenReader(filepath.Join(staging, archive.StoredName))
		if errors.Is(err, zip.ErrFormat) {
			// Preserve compatibility with files that merely use a .zip suffix.
			continue
		}
		if err != nil {
			return nil, total, fmt.Errorf("unable to read %s: %w", archive.OriginalName, err)
		}
		for _, entry := range reader.File {
			clean := path.Clean(strings.ReplaceAll(entry.Name, "\\", "/"))
			if entry.FileInfo().IsDir() {
				continue
			}
			if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || !entry.Mode().IsRegular() {
				reader.Close()
				return nil, total, fmt.Errorf("%s contains unsafe entry %q", archive.OriginalName, entry.Name)
			}
			logicalName := clean
			if seen[logicalName] {
				reader.Close()
				return nil, total, fmt.Errorf("duplicate extracted artifact %q", logicalName)
			}
			if int64(entry.UncompressedSize64) > maxFile || total+int64(entry.UncompressedSize64) > maxBatch {
				reader.Close()
				return nil, total, fmt.Errorf("%w: %s", errExtractTooLarge, logicalName)
			}
			source, err := entry.Open()
			if err != nil {
				reader.Close()
				return nil, total, fmt.Errorf("unable to open %q: %w", logicalName, err)
			}
			stored := fmt.Sprintf("extract-%04d-%x", len(files), sha256.Sum256([]byte(logicalName)))
			destination, err := os.OpenFile(filepath.Join(staging, stored), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
			if err != nil {
				source.Close()
				reader.Close()
				return nil, total, err
			}
			hash := sha256.New()
			n, copyErr := io.Copy(io.MultiWriter(destination, hash), io.LimitReader(source, maxFile+1))
			closeErr := destination.Close()
			source.Close()
			if copyErr != nil || closeErr != nil {
				reader.Close()
				return nil, total, fmt.Errorf("unable to extract %q", logicalName)
			}
			if n > maxFile || total+n > maxBatch {
				reader.Close()
				return nil, total, fmt.Errorf("%w: %s", errExtractTooLarge, logicalName)
			}
			contentType := mime.TypeByExtension(filepath.Ext(clean))
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			files = append(files, ArtifactFile{OriginalName: logicalName, StoredName: stored, Size: n, SHA256: hex.EncodeToString(hash.Sum(nil)), MIMEType: contentType, Kind: "extracted", ArchiveName: archive.OriginalName})
			seen[logicalName] = true
			total += n
		}
		reader.Close()
	}
	return files, total, nil
}
