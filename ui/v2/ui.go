/*
 * Copyright (c) 2021 Gilles Chehade <gilles@poolp.org>
 *
 * Permission to use, copy, modify, and distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */

package v2

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/PlakarLabs/plakar/network"
	"github.com/PlakarLabs/plakar/snapshot"
	"github.com/PlakarLabs/plakar/snapshot/header"
	"github.com/PlakarLabs/plakar/storage"
	"github.com/dustin/go-humanize"
	"github.com/google/uuid"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

var lrepository *storage.Repository

//go:embed frontend/build/*
var content embed.FS

func getConfigHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("received get config request")

	var res network.ResOpen
	config := lrepository.Configuration()
	res.RepositoryConfig = &config
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

type ResGetSnapshots struct {
	Page            uint64            `json:"page"`
	PageSize        uint64            `json:"pageSize"`
	TotalItems      uint64            `json:"totalItems"`
	TotalPages      uint64            `json:"totalPages"`
	HasPreviousPage bool              `json:"hasPreviousPage"`
	HasNextPage     bool              `json:"hasNextPage"`
	Snapshot        string            `json:"snapshot"`
	Path            string            `json:"path"`
	Items           []SnapshotSummary `json:"items"`
}

type SnapshotSummary struct {
	ID        string   `json:"id"`
	ShortID   string   `json:"shortId"`
	Username  string   `json:"username"`
	Hostname  string   `json:"hostName"`
	Location  string   `json:"location"`
	RootPath  string   `json:"rootPath"`
	Date      string   `json:"date"`
	Size      string   `json:"size"`
	Tags      []string `json:"tags"`
	Os        string   `json:"os"`
	Signature string   `json:"signature"`
}

func getSnapshotsHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("received get snapshots request")

	snapshotsIDs, err := lrepository.GetSnapshots()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	headers := make([]header.Header, 0)
	for _, snapshotID := range snapshotsIDs {
		header, _, err := snapshot.GetSnapshot(lrepository, snapshotID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		headers = append(headers, *header)
	}
	sort.Slice(headers, func(i, j int) bool {
		return headers[i].CreationTime.After(headers[j].CreationTime)
	})

	var offsetStr, limitStr string
	if offsetStr = r.URL.Query().Get("offset"); offsetStr == "" {
		offsetStr = "0"
	}
	if limitStr = r.URL.Query().Get("limit"); limitStr == "" {
		limitStr = "10"
	}

	var offset, limit uint64
	if offset, err = strconv.ParseUint(offsetStr, 10, 64); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if limit, err = strconv.ParseUint(limitStr, 10, 64); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if offset >= uint64(len(snapshotsIDs)) {
		offset = 0
	}
	if limit == 0 {
		limit = 10
	}

	fmt.Println(len(snapshotsIDs), offset, limit)
	fmt.Println("Decoded payload", offsetStr, limitStr)

	var res ResGetSnapshots
	res.Page = offset
	res.PageSize = limit
	res.TotalItems = uint64(len(snapshotsIDs))
	res.TotalPages = uint64(len(snapshotsIDs)) / limit
	if uint64(len(snapshotsIDs))%limit != 0 {
		res.TotalPages++
	}
	res.HasPreviousPage = false
	res.HasNextPage = false
	res.Snapshot = ""
	res.Path = ""
	res.Items = []SnapshotSummary{}

	begin := offset
	end := offset + limit
	if end >= uint64(len(snapshotsIDs)) {
		end = uint64(len(snapshotsIDs))
	}

	for _, index := range headers[begin:end] {
		SnapshotSummary := SnapshotSummary{
			ID:        index.IndexID.String(),
			ShortID:   index.GetIndexShortID(),
			Username:  index.Username,
			Hostname:  index.Hostname,
			RootPath:  index.ScannedDirectories[0],
			Date:      index.CreationTime.String(),
			Size:      humanize.Bytes(index.ScanSize),
			Tags:      index.Tags,
			Os:        index.OperatingSystem,
			Signature: "",
		}

		res.Items = append(res.Items, SnapshotSummary)
	}

	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

type ResGetSnapshot struct {
	Page            uint64               `json:"page"`
	PageSize        uint64               `json:"pageSize"`
	TotalItems      uint64               `json:"totalItems"`
	TotalPages      uint64               `json:"totalPages"`
	HasPreviousPage bool                 `json:"hasPreviousPage"`
	HasNextPage     bool                 `json:"hasNextPage"`
	Snapshot        SnapshotSummary      `json:"snapshot"`
	Path            string               `json:"path"`
	Items           []ResGetSnapshotItem `json:"items"`
}

type ResGetSnapshotItem struct {
	Name          string `json:"name"`
	DirectoryPath string `json:"directoryPath"`
	Path          string `json:"path"`
	RawPath       string `json:"rawPath"`
	MimeType      string `json:"mimeType"`
	IsDir         bool   `json:"isDirectory"`
	Mode          string `json:"mode"`
	Uid           string `json:"uid"`
	Gid           string `json:"gid"`
	Date          string `json:"date"`
	Size          string `json:"size"`
	ByteSize      uint64 `json:"byteSize"`
	Checksum      string `json:"checksum"`
	Device        string `json:"device"`
	Inode         string `json:"inode"`
}

func getSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["snapshot"]
	path := vars["path"]

	fmt.Println("received get snapshot request", id, path)

	header, _, err := snapshot.GetSnapshot(lrepository, uuid.MustParse(id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var res ResGetSnapshot
	res.Snapshot = SnapshotSummary{
		ID:        header.IndexID.String(),
		ShortID:   header.GetIndexShortID(),
		Username:  header.Username,
		Hostname:  header.Hostname,
		RootPath:  header.ScannedDirectories[0],
		Date:      header.CreationTime.String(),
		Size:      humanize.Bytes(header.ScanSize),
		Tags:      header.Tags,
		Os:        header.OperatingSystem,
		Signature: "",
	}
	res.Path = path
	res.Items = []ResGetSnapshotItem{}

	fs, _, err := snapshot.GetFilesystem(lrepository, header.VFS[0].Checksum)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	md, _, err := snapshot.GetMetadata(lrepository, header.Metadata[0].Checksum)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	index, _, err := snapshot.GetIndex(lrepository, header.Index[0].Checksum)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	children, err := fs.LookupChildren(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var offsetStr, limitStr string
	if offsetStr = r.URL.Query().Get("offset"); offsetStr == "" {
		offsetStr = "0"
	}
	if limitStr = r.URL.Query().Get("limit"); limitStr == "" {
		limitStr = "10"
	}

	var offset, limit uint64
	if offset, err = strconv.ParseUint(offsetStr, 10, 64); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if limit, err = strconv.ParseUint(limitStr, 10, 64); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if offset >= uint64(len(children)) {
		offset = 0
	}
	if limit == 0 {
		limit = 10
	}

	begin := offset
	end := offset + limit
	if end >= uint64(len(children)) {
		end = uint64(len(children))
	}
	res.Page = offset
	res.PageSize = limit
	res.TotalItems = uint64(len(children))
	res.TotalPages = uint64(len(children)) / limit
	if uint64(len(children))%limit != 0 {
		res.TotalPages++
	}
	res.HasPreviousPage = false
	res.HasNextPage = false

	for _, entry := range children[begin:end] {
		st, err := fs.Lookup(filepath.Join(path, entry))
		if err != nil {
			continue
		}

		ResGetSnapshotItem := ResGetSnapshotItem{
			Name:  entry,
			IsDir: st.Inode.IsDir(),
			Mode:  st.Inode.Lmode.String(),
			Uid:   fmt.Sprintf("%d", st.Inode.Uid()),
			Gid:   fmt.Sprintf("%d", st.Inode.Gid()),
			Date:  st.Inode.LmodTime.String(),
			Size:  humanize.Bytes(uint64(st.Inode.Size())),
		}
		if !ResGetSnapshotItem.IsDir {
			pathChecksum := sha256.Sum256([]byte(filepath.Join(path, entry)))
			object := index.LookupObjectForPathnameChecksum(pathChecksum)
			if object != nil {
				mimeType, _ := md.LookupKeyForValue(object.Checksum)
				if mimeType != "" {
					ResGetSnapshotItem.MimeType = strings.Split(mimeType, ";")[0]
				}
				fmt.Println("mime: [", ResGetSnapshotItem.MimeType, "]")
			}

			ResGetSnapshotItem.Path = filepath.Join(id+":"+path, entry)
			ResGetSnapshotItem.DirectoryPath = id + ":" + path
			ResGetSnapshotItem.RawPath = fmt.Sprintf("http://localhost:3010/api/raw/%s:%s", id, filepath.Join(path, entry))
			ResGetSnapshotItem.ByteSize = uint64(st.Inode.Size())
		} else {
			ResGetSnapshotItem.Path = filepath.Join(id+":"+path, entry, "") + "/"
		}
		fmt.Println("adding", ResGetSnapshotItem)
		res.Items = append(res.Items, ResGetSnapshotItem)
	}

	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

}

func getRawHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["snapshot"]
	path := vars["path"]

	fmt.Println("received get raw request", id, path)

	snap, err := snapshot.Load(lrepository, uuid.MustParse(id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var mimeType string

	pathChecksum := sha256.Sum256([]byte(path))
	object := snap.Index.LookupObjectForPathnameChecksum(pathChecksum)
	if object != nil {
		mimeType, _ = snap.Metadata.LookupKeyForValue(object.Checksum)
		if mimeType != "" {
			mimeType = strings.Split(mimeType, ";")[0]
		}
		fmt.Println("mime:", mimeType)
	}

	rd, err := snapshot.NewReader(snap, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", mimeType)
	download := ""
	if download != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(path)))
	}
	io.Copy(w, rd)
}

func Ui(repository *storage.Repository, addr string, spawn bool) error {
	lrepository = repository

	var url string
	if addr != "" {
		url = fmt.Sprintf("http://%s", addr)
	} else {
		var port uint16
		for {
			port = uint16(rand.Uint32() % 0xffff)
			if port >= 1024 {
				break
			}
		}
		addr = fmt.Sprintf("localhost:%d", port)
		url = fmt.Sprintf("http://%s", addr)
	}
	var err error
	fmt.Println("lauching browser UI pointing at", url)
	if spawn {
		switch runtime.GOOS {
		case "windows":
			err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		case "darwin":
			err = exec.Command("open", url).Start()
		default: // "linux", "freebsd", "openbsd", "netbsd"
			err = exec.Command("xdg-open", url).Start()
		}
		if err != nil {
			return err
		}
	}

	r := mux.NewRouter()

	r.PathPrefix("/api/config").HandlerFunc(getConfigHandler).Methods("GET")
	r.PathPrefix("/api/snapshots").HandlerFunc(getSnapshotsHandler).Methods("GET")
	r.PathPrefix("/api/snapshot/{snapshot}:{path:.+}/").HandlerFunc(getSnapshotHandler).Methods("GET")
	r.PathPrefix("/api/snapshot/{snapshot}:/").HandlerFunc(getSnapshotHandler).Methods("GET")

	r.PathPrefix("/api/raw/{snapshot}:{path:.+}").HandlerFunc(getRawHandler).Methods("GET")

	r.PathPrefix("/static/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip the "/static/" prefix from the request path
		httpPath := r.URL.Path

		// Read the file from the embedded content
		data, err := content.ReadFile("frontend/build" + httpPath)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}

		// Determine the content type based on the file extension
		contentType := ""
		switch {
		case strings.HasSuffix(httpPath, ".css"):
			contentType = "text/css"
		case strings.HasSuffix(httpPath, ".js"):
			contentType = "application/javascript"
		case strings.HasSuffix(httpPath, ".png"):
			contentType = "image/png"
		case strings.HasSuffix(httpPath, ".jpg"), strings.HasSuffix(httpPath, ".jpeg"):
			contentType = "image/jpeg"
			// Add more content types as needed
		}

		// Set the content type header
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}

		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})

	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := content.ReadFile("frontend/build/index.html")
		if err != nil {
			http.Error(w, "App not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})

	return http.ListenAndServe(addr, handlers.CORS()(r))
}
