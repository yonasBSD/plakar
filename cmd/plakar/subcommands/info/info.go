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

package info

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/PlakarKorp/plakar/cmd/plakar/subcommands"
	"github.com/PlakarKorp/plakar/cmd/plakar/utils"
	"github.com/PlakarKorp/plakar/context"
	"github.com/PlakarKorp/plakar/logger"
	"github.com/PlakarKorp/plakar/objects"
	"github.com/PlakarKorp/plakar/packfile"
	"github.com/PlakarKorp/plakar/repository"
	"github.com/PlakarKorp/plakar/repository/state"
	"github.com/PlakarKorp/plakar/snapshot/vfs"
	"github.com/dustin/go-humanize"
)

func init() {
	subcommands.Register("info", cmd_info)
}

func cmd_info(ctx *context.Context, repo *repository.Repository, args []string) int {
	if len(args) == 0 {
		return info_repository(repo)
	}

	flags := flag.NewFlagSet("info", flag.ExitOnError)
	flags.Parse(args)

	// Determine which concept to show information for based on flags.Args()[0]
	switch flags.Arg(0) {
	case "snapshot":
		if len(flags.Args()) < 2 {
			logger.Error("usage: %s snapshot snapshotID", flags.Name())
			return 1
		}
		if err := info_snapshot(repo, flags.Args()[1]); err != nil {
			logger.Error("error: %s", err)
			return 1
		}

	case "state":
		if err := info_state(repo, flags.Args()[1:]); err != nil {
			logger.Error("error: %s", err)
			return 1
		}

	case "packfile":
		if err := info_packfile(repo, flags.Args()[1:]); err != nil {
			logger.Error("error: %s", err)
			return 1
		}

	case "object":
		if len(flags.Args()) < 2 {
			logger.Error("usage: %s object objectID", flags.Name())
			return 1
		}
		if err := info_object(repo, flags.Args()[1]); err != nil {
			logger.Error("error: %s", err)
			return 1
		}

	case "vfs":
		if len(flags.Args()) < 2 {
			logger.Error("usage: %s vfs snapshotPathname", flags.Name())
			return 1
		}
		if err := info_vfs(repo, flags.Args()[1]); err != nil {
			logger.Error("error: %s", err)
			return 1
		}

	default:
		fmt.Println("Invalid parameter. usage: info [snapshot|object|chunk|state|packfile|vfs]")
		return 1
	}

	return 0
}

func info_repository(repo *repository.Repository) int {
	metadatas, err := utils.GetHeaders(repo, nil)
	if err != nil {
		logger.Warn("%s", err)
		return 1
	}

	fmt.Println("Version:", repo.Configuration().Version)
	fmt.Println("CreationTime:", repo.Configuration().CreationTime)
	fmt.Println("RepositoryID:", repo.Configuration().RepositoryID)

	fmt.Println("Packfile:")
	fmt.Printf(" - MaxSize: %s (%d bytes)\n",
		humanize.Bytes(uint64(repo.Configuration().Packfile.MaxSize)),
		repo.Configuration().Packfile.MaxSize)

	fmt.Println("Chunking:")
	fmt.Println(" - Algorithm:", repo.Configuration().Chunking.Algorithm)
	fmt.Printf(" - MinSize: %s (%d bytes)\n",
		humanize.Bytes(uint64(repo.Configuration().Chunking.MinSize)), repo.Configuration().Chunking.MinSize)
	fmt.Printf(" - NormalSize: %s (%d bytes)\n",
		humanize.Bytes(uint64(repo.Configuration().Chunking.NormalSize)), repo.Configuration().Chunking.NormalSize)
	fmt.Printf(" - MaxSize: %s (%d bytes)\n",
		humanize.Bytes(uint64(repo.Configuration().Chunking.MaxSize)), repo.Configuration().Chunking.MaxSize)

	fmt.Println("Hashing:")
	fmt.Println(" - Algorithm:", repo.Configuration().Hashing.Algorithm)
	fmt.Println(" - Bits:", repo.Configuration().Hashing.Bits)

	if repo.Configuration().Compression != nil {
		fmt.Println("Compression:")
		fmt.Println(" - Algorithm:", repo.Configuration().Compression.Algorithm)
		fmt.Println(" - Level:", repo.Configuration().Compression.Level)
	}

	if repo.Configuration().Encryption != nil {
		fmt.Println("Encryption:")
		fmt.Println(" - Algorithm:", repo.Configuration().Encryption.Algorithm)
		fmt.Println(" - Key:", repo.Configuration().Encryption.Key)
	}

	fmt.Println("Snapshots:", len(metadatas))
	totalSize := uint64(0)
	for _, metadata := range metadatas {
		totalSize += metadata.ScanProcessedSize
	}
	fmt.Printf("Size: %s (%d bytes)\n", humanize.Bytes(totalSize), totalSize)

	return 0
}

func info_snapshot(repo *repository.Repository, snapshotID string) error {

	snap, err := utils.OpenSnapshotByPrefix(repo, snapshotID)
	if err != nil {
		return err
	}

	header := snap.Header

	indexID := header.GetIndexID()
	fmt.Printf("SnapshotID: %s\n", hex.EncodeToString(indexID[:]))
	fmt.Printf("CreationTime: %s\n", header.CreationTime)
	fmt.Printf("CreationDuration: %s\n", header.CreationDuration)

	fmt.Printf("Root: %x\n", header.Root)
	fmt.Printf("Metadata: %x\n", header.Metadata)
	fmt.Printf("Statistics: %x\n", header.Statistics)

	fmt.Printf("Version: %s\n", repo.Configuration().Version)
	fmt.Printf("Hostname: %s\n", header.Hostname)
	fmt.Printf("Username: %s\n", header.Username)
	fmt.Printf("CommandLine: %s\n", header.CommandLine)
	fmt.Printf("OperatingSystem: %s\n", header.OperatingSystem)
	fmt.Printf("Architecture: %s\n", header.Architecture)
	fmt.Printf("NumCPU: %d\n", header.NumCPU)

	fmt.Printf("Type: %s\n", header.Type)
	fmt.Printf("Origin: %s\n", header.Origin)

	fmt.Printf("MachineID: %s\n", header.MachineID)
	fmt.Printf("PublicKey: %s\n", header.PublicKey)
	fmt.Printf("Tags: %s\n", strings.Join(header.Tags, ", "))
	fmt.Printf("Directories: %d\n", header.DirectoriesCount)
	fmt.Printf("Files: %d\n", header.FilesCount)

	fmt.Printf("Snapshot.Size: %s (%d bytes)\n", humanize.Bytes(header.ScanProcessedSize), header.ScanProcessedSize)
	return nil
}

func info_state(repo *repository.Repository, args []string) error {
	if len(args) == 0 {
		states, err := repo.GetStates()
		if err != nil {
			log.Fatal(err)
		}

		for _, state := range states {
			fmt.Printf("%x\n", state)
		}
	} else {
		for _, arg := range args {
			// convert arg to [32]byte
			if len(arg) != 64 {
				log.Fatalf("invalid packfile hash: %s", arg)
			}

			b, err := hex.DecodeString(arg)
			if err != nil {
				log.Fatalf("invalid packfile hash: %s", arg)
			}

			// Convert the byte slice to a [32]byte
			var byteArray [32]byte
			copy(byteArray[:], b)

			rawState, _, err := repo.GetState(byteArray)
			if err != nil {
				log.Fatal(err)
			}

			st, err := state.NewFromBytes(rawState)
			if err != nil {
				log.Fatal(err)
			}

			fmt.Printf("Version: %d.%d.%d\n", st.Metadata.Version/100, (st.Metadata.Version/10)%10, st.Metadata.Version%10)
			fmt.Printf("Creation: %s\n", st.Metadata.CreationTime)
			if len(st.Metadata.Extends) > 0 {
				fmt.Printf("Extends:\n")
				for _, stateID := range st.Metadata.Extends {
					fmt.Printf("  %x\n", stateID)
				}
			}

			for snapshotID, subpart := range st.Snapshots {
				fmt.Printf("snapshot %x : packfile %x, offset %d, length %d\n",
					st.IdToChecksum[snapshotID],
					st.IdToChecksum[subpart.Packfile],
					subpart.Offset,
					subpart.Length)
			}

			for chunk, subpart := range st.Chunks {
				fmt.Printf("chunk %x : packfile %x, offset %d, length %d\n",
					st.IdToChecksum[chunk],
					st.IdToChecksum[subpart.Packfile],
					subpart.Offset,
					subpart.Length)
			}

			for object, subpart := range st.Objects {
				fmt.Printf("object %x : packfile %x, offset %d, length %d\n",
					st.IdToChecksum[object],
					st.IdToChecksum[subpart.Packfile],
					subpart.Offset,
					subpart.Length)
			}

			for file, subpart := range st.Files {
				fmt.Printf("file %x : packfile %x, offset %d, length %d\n",
					st.IdToChecksum[file],
					st.IdToChecksum[subpart.Packfile],
					subpart.Offset,
					subpart.Length)
			}

			for directory, subpart := range st.Directories {
				fmt.Printf("directory %x : packfile %x, offset %d, length %d\n",
					st.IdToChecksum[directory],
					st.IdToChecksum[subpart.Packfile],
					subpart.Offset,
					subpart.Length)
			}

			for data, subpart := range st.Datas {
				fmt.Printf("data %x : packfile %x, offset %d, length %d\n",
					st.IdToChecksum[data],
					st.IdToChecksum[subpart.Packfile],
					subpart.Offset,
					subpart.Length)
			}
		}
	}
	return nil
}

func info_packfile(repo *repository.Repository, args []string) error {
	if len(args) == 0 {
		packfiles, err := repo.GetPackfiles()
		if err != nil {
			log.Fatal(err)
		}

		for _, packfile := range packfiles {
			fmt.Printf("%x\n", packfile)
		}
	} else {
		for _, arg := range args {
			// convert arg to [32]byte
			if len(arg) != 64 {
				log.Fatalf("invalid packfile hash: %s", arg)
			}

			b, err := hex.DecodeString(arg)
			if err != nil {
				log.Fatalf("invalid packfile hash: %s", arg)
			}

			// Convert the byte slice to a [32]byte
			var byteArray [32]byte
			copy(byteArray[:], b)

			rd, _, err := repo.GetPackfile(byteArray)
			if err != nil {
				log.Fatal(err)
			}

			rawPackfile, err := io.ReadAll(rd)
			if err != nil {
				log.Fatal(err)
			}

			versionBytes := rawPackfile[len(rawPackfile)-5 : len(rawPackfile)-5+4]
			version := binary.LittleEndian.Uint32(versionBytes)

			//			version := rawPackfile[len(rawPackfile)-2]
			footerOffset := rawPackfile[len(rawPackfile)-1]
			rawPackfile = rawPackfile[:len(rawPackfile)-5]

			_ = version

			footerbuf := rawPackfile[len(rawPackfile)-int(footerOffset):]
			rawPackfile = rawPackfile[:len(rawPackfile)-int(footerOffset)]

			footerbuf, err = repo.Decode(footerbuf)
			if err != nil {
				log.Fatal(err)
			}
			footer, err := packfile.NewFooterFromBytes(footerbuf)
			if err != nil {
				log.Fatal(err)
			}

			indexbuf := rawPackfile[int(footer.IndexOffset):]
			rawPackfile = rawPackfile[:int(footer.IndexOffset)]

			indexbuf, err = repo.Decode(indexbuf)
			if err != nil {
				log.Fatal(err)
			}

			hasher := sha256.New()
			hasher.Write(indexbuf)

			if !bytes.Equal(hasher.Sum(nil), footer.IndexChecksum[:]) {
				log.Fatal("index checksum mismatch")
			}

			rawPackfile = append(rawPackfile, indexbuf...)
			rawPackfile = append(rawPackfile, footerbuf...)

			p, err := packfile.NewFromBytes(rawPackfile)
			if err != nil {
				log.Fatal(err)
			}

			fmt.Printf("Version: %d.%d.%d\n", p.Footer.Version/100, p.Footer.Version%100/10, p.Footer.Version%10)
			fmt.Printf("Timestamp: %s\n", time.Unix(0, p.Footer.Timestamp))
			fmt.Printf("Index checksum: %x\n", p.Footer.IndexChecksum)
			fmt.Println()

			for i, entry := range p.Index {
				fmt.Printf("blob[%d]: %x %d %d %s\n", i, entry.Checksum, entry.Offset, entry.Length, entry.TypeName())
			}
		}
	}
	return nil
}

func info_object(repo *repository.Repository, objectID string) error {
	if len(objectID) != 64 {
		log.Fatalf("invalid object hash: %s", objectID)
	}

	b, err := hex.DecodeString(objectID)
	if err != nil {
		log.Fatalf("invalid object hash: %s", objectID)
	}

	// Convert the byte slice to a [32]byte
	var byteArray [32]byte
	copy(byteArray[:], b)

	rd, _, err := repo.GetObject(byteArray)
	if err != nil {
		log.Fatal(err)
	}

	blob, err := io.ReadAll(rd)
	if err != nil {
		log.Fatal(err)
	}

	object, err := objects.NewObjectFromBytes(blob)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("object: %x\n", object.Checksum)
	fmt.Println("  type:", object.ContentType)
	if len(object.Tags) > 0 {
		fmt.Println("  tags:", strings.Join(object.Tags, ","))
	}

	fmt.Println("  chunks:")
	for _, chunk := range object.Chunks {
		fmt.Printf("    checksum: %x\n", chunk.Checksum)
	}
	return nil
}

func info_vfs(repo *repository.Repository, snapshotPath string) error {
	// TODO
	snapshotPrefix, pathname := utils.ParseSnapshotID(snapshotPath)
	snap1, err := utils.OpenSnapshotByPrefix(repo, snapshotPrefix)
	if err != nil {
		return err
	}
	fs, err := snap1.Filesystem()
	if err != nil {
		return err
	}

	fsinfo, err := fs.Stat(filepath.Clean(pathname))
	if err != nil {
		return err
	}

	if dirEntry, isDir := fsinfo.(*vfs.DirEntry); isDir {
		fmt.Printf("[DirEntry]\n")
		fmt.Printf("Version: %d\n", dirEntry.Version)
		fmt.Printf("ParentPath: %s\n", dirEntry.ParentPath)
		fmt.Printf("Name: %s\n", dirEntry.Stat().Name())
		fmt.Printf("Type: %d\n", dirEntry.Type)
		fmt.Printf("Size: %s (%d bytes)\n", humanize.Bytes(uint64(dirEntry.Stat().Size())), dirEntry.Stat().Size())
		fmt.Printf("Permissions: %s\n", dirEntry.Stat().Mode())
		fmt.Printf("ModTime: %s\n", dirEntry.Stat().ModTime())
		fmt.Printf("DeviceID: %d\n", dirEntry.Stat().Dev())
		fmt.Printf("InodeID: %d\n", dirEntry.Stat().Ino())
		fmt.Printf("UserID: %d\n", dirEntry.Stat().Uid())
		fmt.Printf("GroupID: %d\n", dirEntry.Stat().Gid())
		fmt.Printf("Username: %s\n", dirEntry.Stat().Username())
		fmt.Printf("Groupname: %s\n", dirEntry.Stat().Groupname())
		fmt.Printf("NumLinks: %d\n", dirEntry.Stat().Nlink())
		fmt.Printf("ExtendedAttributes: %s\n", dirEntry.ExtendedAttributes)
		fmt.Printf("CustomMetadata: %s\n", dirEntry.CustomMetadata)
		fmt.Printf("Tags: %s\n", dirEntry.Tags)
		fmt.Printf("Below.Directories: %d\n", dirEntry.Statistics.Below.Directories)
		fmt.Printf("Below.Files: %d\n", dirEntry.Statistics.Below.Files)
		fmt.Printf("Below.Symlinks: %d\n", dirEntry.Statistics.Below.Symlinks)
		fmt.Printf("Below.Devices: %d\n", dirEntry.Statistics.Below.Devices)
		fmt.Printf("Below.Pipes: %d\n", dirEntry.Statistics.Below.Pipes)
		fmt.Printf("Below.Sockets: %d\n", dirEntry.Statistics.Below.Sockets)
		fmt.Printf("Below.Objects: %d\n", dirEntry.Statistics.Below.Objects)
		fmt.Printf("Below.Chunks: %d\n", dirEntry.Statistics.Below.Chunks)
		fmt.Printf("Below.MinSize: %s (%d bytes)\n", humanize.Bytes(uint64(dirEntry.Statistics.Below.MinSize)), dirEntry.Statistics.Below.MinSize)
		fmt.Printf("Below.MaxSize: %s (%d bytes)\n", humanize.Bytes(uint64(dirEntry.Statistics.Below.MaxSize)), dirEntry.Statistics.Below.MaxSize)
		fmt.Printf("Below.Size: %s (%d bytes)\n", humanize.Bytes(uint64(dirEntry.Statistics.Below.Size)), dirEntry.Statistics.Below.Size)
		fmt.Printf("Below.MinModTime: %s\n", time.Unix(dirEntry.Statistics.Below.MinModTime, 0))
		fmt.Printf("Below.MaxModTime: %s\n", time.Unix(dirEntry.Statistics.Below.MaxModTime, 0))
		fmt.Printf("Below.MinEntropy: %f\n", dirEntry.Statistics.Below.MinEntropy)
		fmt.Printf("Below.MaxEntropy: %f\n", dirEntry.Statistics.Below.MaxEntropy)
		fmt.Printf("Below.HiEntropy: %d\n", dirEntry.Statistics.Below.HiEntropy)
		fmt.Printf("Below.LoEntropy: %d\n", dirEntry.Statistics.Below.LoEntropy)
		fmt.Printf("Below.MIMEAudio: %d\n", dirEntry.Statistics.Below.MIMEAudio)
		fmt.Printf("Below.MIMEVideo: %d\n", dirEntry.Statistics.Below.MIMEVideo)
		fmt.Printf("Below.MIMEImage: %d\n", dirEntry.Statistics.Below.MIMEImage)
		fmt.Printf("Below.MIMEText: %d\n", dirEntry.Statistics.Below.MIMEText)
		fmt.Printf("Below.MIMEApplication: %d\n", dirEntry.Statistics.Below.MIMEApplication)
		fmt.Printf("Below.MIMEOther: %d\n", dirEntry.Statistics.Below.MIMEOther)

		fmt.Printf("Directory.Directories: %d\n", dirEntry.Statistics.Directory.Directories)
		fmt.Printf("Directory.Files: %d\n", dirEntry.Statistics.Directory.Files)
		fmt.Printf("Directory.Symlinks: %d\n", dirEntry.Statistics.Directory.Symlinks)
		fmt.Printf("Directory.Devices: %d\n", dirEntry.Statistics.Directory.Devices)
		fmt.Printf("Directory.Pipes: %d\n", dirEntry.Statistics.Directory.Pipes)
		fmt.Printf("Directory.Sockets: %d\n", dirEntry.Statistics.Directory.Sockets)
		fmt.Printf("Directory.Objects: %d\n", dirEntry.Statistics.Directory.Objects)
		fmt.Printf("Directory.Chunks: %d\n", dirEntry.Statistics.Directory.Chunks)
		fmt.Printf("Directory.MinSize: %s (%d bytes)\n", humanize.Bytes(uint64(dirEntry.Statistics.Directory.MinSize)), dirEntry.Statistics.Directory.MinSize)
		fmt.Printf("Directory.MaxSize: %s (%d bytes)\n", humanize.Bytes(uint64(dirEntry.Statistics.Directory.MaxSize)), dirEntry.Statistics.Directory.MaxSize)
		fmt.Printf("Directory.Size: %s (%d bytes)\n", humanize.Bytes(uint64(dirEntry.Statistics.Directory.Size)), dirEntry.Statistics.Directory.Size)
		fmt.Printf("Directory.MinModTime: %s\n", time.Unix(dirEntry.Statistics.Directory.MinModTime, 0))
		fmt.Printf("Directory.MaxModTime: %s\n", time.Unix(dirEntry.Statistics.Directory.MaxModTime, 0))
		fmt.Printf("Directory.MinEntropy: %f\n", dirEntry.Statistics.Directory.MinEntropy)
		fmt.Printf("Directory.MaxEntropy: %f\n", dirEntry.Statistics.Directory.MaxEntropy)
		fmt.Printf("Directory.AvgEntropy: %f\n", dirEntry.Statistics.Directory.AvgEntropy)
		fmt.Printf("Directory.HiEntropy: %d\n", dirEntry.Statistics.Directory.HiEntropy)
		fmt.Printf("Directory.LoEntropy: %d\n", dirEntry.Statistics.Directory.LoEntropy)
		fmt.Printf("Directory.MIMEAudio: %d\n", dirEntry.Statistics.Directory.MIMEAudio)
		fmt.Printf("Directory.MIMEVideo: %d\n", dirEntry.Statistics.Directory.MIMEVideo)
		fmt.Printf("Directory.MIMEImage: %d\n", dirEntry.Statistics.Directory.MIMEImage)
		fmt.Printf("Directory.MIMEText: %d\n", dirEntry.Statistics.Directory.MIMEText)
		fmt.Printf("Directory.MIMEApplication: %d\n", dirEntry.Statistics.Directory.MIMEApplication)
		fmt.Printf("Directory.MIMEOther: %d\n", dirEntry.Statistics.Directory.MIMEOther)

		for offset, child := range dirEntry.Children {
			fmt.Printf("Child[%d].Checksum: %x\n", offset, child.Checksum())
			fmt.Printf("Child[%d].FileInfo.Name(): %s\n", offset, child.Stat().Name())
			fmt.Printf("Child[%d].FileInfo.Size(): %d\n", offset, child.Stat().Size())
			fmt.Printf("Child[%d].FileInfo.Mode(): %s\n", offset, child.Stat().Mode())
			fmt.Printf("Child[%d].FileInfo.Dev(): %d\n", offset, child.Stat().Dev())
			fmt.Printf("Child[%d].FileInfo.Ino(): %d\n", offset, child.Stat().Ino())
			fmt.Printf("Child[%d].FileInfo.Uid(): %d\n", offset, child.Stat().Uid())
			fmt.Printf("Child[%d].FileInfo.Gid(): %d\n", offset, child.Stat().Gid())
			fmt.Printf("Child[%d].FileInfo.Username(): %s\n", offset, child.Stat().Username())
			fmt.Printf("Child[%d].FileInfo.Groupname(): %s\n", offset, child.Stat().Groupname())
			fmt.Printf("Child[%d].FileInfo.Nlink(): %d\n", offset, child.Stat().Nlink())
		}

	} else if fileEntry, isFile := fsinfo.(*vfs.FileEntry); isFile {
		fmt.Printf("[FileEntry]\n")
		fmt.Printf("Version: %d\n", fileEntry.Version)
		fmt.Printf("ParentPath: %s\n", fileEntry.ParentPath)
		fmt.Printf("Name: %s\n", fileEntry.Stat().Name())
		fmt.Printf("Type: %d\n", fileEntry.Type)
		fmt.Printf("Size: %s (%d bytes)\n", humanize.Bytes(uint64(fileEntry.Stat().Size())), fileEntry.Stat().Size())
		fmt.Printf("Permissions: %s\n", fileEntry.Stat().Mode())
		fmt.Printf("ModTime: %s\n", fileEntry.Stat().ModTime())
		fmt.Printf("DeviceID: %d\n", fileEntry.Stat().Dev())
		fmt.Printf("InodeID: %d\n", fileEntry.Stat().Ino())
		fmt.Printf("UserID: %d\n", fileEntry.Stat().Uid())
		fmt.Printf("GroupID: %d\n", fileEntry.Stat().Gid())
		fmt.Printf("Username: %s\n", fileEntry.Stat().Username())
		fmt.Printf("Groupname: %s\n", fileEntry.Stat().Groupname())
		fmt.Printf("NumLinks: %d\n", fileEntry.Stat().Nlink())
		fmt.Printf("ExtendedAttributes: %s\n", fileEntry.ExtendedAttributes)
		fmt.Printf("FileAttributes: %v\n", fileEntry.FileAttributes)
		if fileEntry.SymlinkTarget != "" {
			fmt.Printf("SymlinkTarget: %s\n", fileEntry.SymlinkTarget)
		}
		fmt.Printf("CustomMetadata: %s\n", fileEntry.CustomMetadata)
		fmt.Printf("Tags: %s\n", fileEntry.Tags)
		fmt.Printf("Checksum: %x\n", fileEntry.Object.Checksum)
		for offset, chunk := range fileEntry.Object.Chunks {
			fmt.Printf("Chunk[%d].Checksum: %x\n", offset, chunk.Checksum)
			fmt.Printf("Chunk[%d].Length: %d\n", offset, chunk.Length)
		}
	}
	return nil
}
