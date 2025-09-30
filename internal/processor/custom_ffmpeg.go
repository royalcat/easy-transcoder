package processor

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
	"github.com/ulikunitz/xz/lzma"
)

func downloadAndExtract(archiveUrl string, ffmpegBinaryPath string) error {
	url, err := url.Parse(archiveUrl)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}
	filename := path.Base(url.Path)
	if filename == "" || filename == "/" {
		return fmt.Errorf("invalid URL, last part not containing a filename: %s", archiveUrl)
	}

	resp, err := http.Get(archiveUrl)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	archiveData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read archive data: %w", err)
	}

	// archiveFilePath := path.Join(dir, filename)
	// archiveFile, err := os.OpenFile(archiveFilePath, os.O_CREATE|os.O_RDWR, 0644)
	// if err != nil {
	// 	return fmt.Errorf("failed to create archive file: %w", err)
	// }
	// defer archiveFile.Close()

	// _, err = io.Copy(archiveFile, resp.Body)
	// if err != nil {
	// 	return fmt.Errorf("failed to copy file: %w", err)
	// }
	//

	if strings.HasSuffix(filename, ".zip") {
		zipReader, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
		if err != nil {
			return fmt.Errorf("failed to create zip reader: %w", err)
		}

		var ffmpegZipFile *zip.File
		for _, file := range zipReader.File {
			if strings.HasSuffix(file.Name, "/ffmpeg") {
				ffmpegZipFile = file
				break
			}
		}
		if ffmpegZipFile == nil {
			return fmt.Errorf("ffmpeg binary not found in archive")
		}

		reader, err := ffmpegZipFile.Open()
		if err != nil {
			return fmt.Errorf("failed to open ffmpeg file: %w", err)
		}

		ffmpegFile, err := os.OpenFile(ffmpegBinaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
		if err != nil {
			return fmt.Errorf("failed to create ffmpeg file: %w", err)
		}
		defer ffmpegFile.Close()

		_, err = io.Copy(ffmpegFile, reader)
		if err != nil {
			return fmt.Errorf("failed to copy ffmpeg file: %w", err)
		}
	} else if parts := strings.Split(filename, "."); (len(parts) >= 3 && parts[len(parts)-2] == "tar") || strings.HasSuffix(filename, ".tar") {
		var reader io.Reader
		switch parts[len(parts)-1] {
		case "tar":
			reader = bytes.NewReader(archiveData)
		case "gz":
			reader, err = gzip.NewReader(bytes.NewReader(archiveData))
			if err != nil {
				return fmt.Errorf("failed to create gzip reader: %w", err)
			}
		case "xz":
			reader, err = xz.NewReader(bytes.NewReader(archiveData))
			if err != nil {
				return fmt.Errorf("failed to create xz reader: %w", err)
			}
		case "zstd":
			reader, err = zstd.NewReader(bytes.NewReader(archiveData))
			if err != nil {
				return fmt.Errorf("failed to create zstd reader: %w", err)
			}
		case "lzma":
			reader, err = lzma.NewReader(bytes.NewReader(archiveData))
			if err != nil {
				return fmt.Errorf("failed to create lzma reader: %w", err)
			}
		default:
			return fmt.Errorf("unsupported compression format: %s", parts[len(parts)-1])
		}

		tarReader := tar.NewReader(reader)

		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("failed to read tar header: %w", err)
			}

			if strings.HasSuffix(header.Name, "ffmpeg") {
				ffmpegFile, err := os.OpenFile(ffmpegBinaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
				if err != nil {
					return fmt.Errorf("failed to create ffmpeg file: %w", err)
				}
				defer ffmpegFile.Close()

				_, err = io.CopyN(ffmpegFile, tarReader, int64(header.Size))
				if err != nil {
					return fmt.Errorf("failed to copy ffmpeg file: %w", err)
				}
				return nil
			}
		}

		return fmt.Errorf("ffmpeg binary not found in archive")
	}

	return nil
}
