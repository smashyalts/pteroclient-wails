package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"pteroclient-wails/pkg/mcp"
	"pteroclient-wails/pkg/pteroapi"
)

// registerFileTools covers every file route the client API has, plus the
// recursive search it does not have.
func (t *toolset) registerFileTools(s *mcp.Server) {
	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_files_list",
		Description: "List one directory on a server. Paths are absolute from the server root, " +
			"which is '/'. Does not recurse; use ptero_files_search to look through a tree.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("directory", "Directory to list, e.g. '/' or '/plugins'.").Def("/"),
			mcp.Bool("as_json", "Return the panel's full records instead of the compact table.").Def(false),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			directory := args.String("directory", "/")

			if args.Bool("as_json", false) {
				query := url.Values{}
				query.Set("directory", directory)
				return t.call(ctx, args, http.MethodGet, "/files/list", query, nil)
			}

			client, server, err := t.target(args)
			if err != nil {
				return "", err
			}
			entries, err := client.ListDirectory(ctx, server, directory)
			if err != nil {
				return "", err
			}
			return renderListing(directory, entries), nil
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_files_read",
		Description: "Read a text file from a server. Large files are truncated rather than " +
			"returned whole; use start_line and line_count to page through one, or " +
			"ptero_files_download_url for a binary. The panel refuses this route for very large " +
			"files, which is also why the download URL exists.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("file", "Absolute path of the file, e.g. '/server.properties'.").Req(),
			mcp.Int("max_bytes", "Most bytes to return before truncating.").Def(65536),
			mcp.Int("start_line", "First line to return, 1-based. Omit for the start of the file."),
			mcp.Int("line_count", "How many lines to return from start_line."),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			client, server, err := t.target(args)
			if err != nil {
				return "", err
			}
			path := args.String("file", "")
			content, err := client.ReadFile(ctx, server, path)
			if err != nil {
				return "", err
			}
			return renderFile(path, content, args.Int("start_line", 0), args.Int("line_count", 0),
				args.Int("max_bytes", 65536)), nil
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_files_search",
		Description: "Search a server's files by name, by content, or both, walking the tree. The " +
			"panel has no search route, so this is the way to answer 'which file sets this option' " +
			"or 'where is that error logged'. Give name alone to find files, contains alone to grep " +
			"every file, or both to grep only the files whose names match — which is far quicker.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("name", "Glob matched against file names, case insensitively: '*.yml', '*log*'. "+
				"A plain word is treated as a substring."),
			mcp.Str("contains", "Text to find inside matching files."),
			mcp.Bool("case_sensitive", "Match contains case-sensitively.").Def(false),
			mcp.Str("root", "Directory to search from.").Def("/"),
			mcp.Int("max_depth", "How many directory levels to descend.").Def(6),
			mcp.Int("max_results", "Stop after this many matches.").Def(100),
			mcp.Int("max_file_kb", "Skip files larger than this when searching content.").Def(512),
			mcp.StrList("skip_dirs", "Directory names never descended into. Defaults to caches, "+
				"libraries and world region folders, which are large and never what is wanted."),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			client, server, err := t.target(args)
			if err != nil {
				return "", err
			}

			opts := pteroapi.SearchOptions{
				Server:        server,
				Root:          args.String("root", "/"),
				Name:          args.String("name", ""),
				Contains:      args.String("contains", ""),
				CaseSensitive: args.Bool("case_sensitive", false),
				MaxDepth:      args.Int("max_depth", 6),
				MaxResults:    args.Int("max_results", 100),
				MaxFileBytes:  int64(args.Int("max_file_kb", 512)) * 1024,
				SkipDirs:      args.StringList("skip_dirs"),
			}

			result, err := client.Search(ctx, opts)
			if err != nil {
				return "", err
			}
			return renderSearch(result), nil
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_files_download_url",
		Description: "Get a short-lived signed URL for downloading a file, which works for binaries " +
			"and for files too large for the read route. The URL points at the node and carries its " +
			"own token, so it needs no API key.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("file", "Absolute path of the file to download.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			query := url.Values{}
			query.Set("file", args.String("file", ""))
			return t.call(ctx, args, http.MethodGet, "/files/download", query, nil)
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_files_upload_url",
		Description: "Get a short-lived signed URL for uploading files to a server. POST a multipart " +
			"form to it with the field name 'files' and a '&directory=' parameter for the target folder.",
		Input: mcp.In(panelField, serverField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodGet, "/files/upload", nil, nil)
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_files_write",
		Description: "Write a text file on a server, creating it or replacing it entirely. There is " +
			"no partial write and no diff: whatever is sent becomes the whole file, so read the file " +
			"first unless you mean to replace it. Most servers only reload a config on restart.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("file", "Absolute path to write, e.g. '/plugins/Essentials/config.yml'.").Req(),
			mcp.Str("content", "The file's complete new contents.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			client, server, err := t.target(args)
			if err != nil {
				return "", err
			}
			path := args.String("file", "")
			content := args.String("content", "")

			query := url.Values{}
			query.Set("file", path)

			// The write route takes the bytes as the body rather than as a
			// JSON field, so this cannot go through the JSON helper.
			result, err := client.Send(ctx, pteroapi.Request{
				Method:  http.MethodPost,
				Path:    "/servers/" + server + "/files/write",
				Query:   query,
				Text:    content,
				UseText: true,
			})
			if err != nil {
				return "", err
			}
			if result.Status < 200 || result.Status > 299 {
				return "", fmt.Errorf("panel returned %d: %s", result.Status, strings.TrimSpace(string(result.Body)))
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name:        "ptero_files_create_folder",
		Description: "Create a directory on a server.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("root", "Directory to create it in.").Def("/"),
			mcp.Str("name", "Name of the new directory. Nested paths are allowed.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodPost, "/files/create-folder", nil, map[string]string{
				"root": args.String("root", "/"),
				"name": args.String("name", ""),
			})
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_files_rename",
		Description: "Rename or move files and directories. Both names are relative to root, so a " +
			"'to' of 'old/config.yml' moves the file into a subdirectory. Moving onto an existing " +
			"path replaces it.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("root", "Directory both names are relative to.").Def("/"),
			mcp.Str("from", "Current name, relative to root."),
			mcp.Str("to", "New name, relative to root."),
			mcp.Obj("renames", "For several at once: an object of current name to new name, instead "+
				"of from and to."),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			pairs := make([]map[string]string, 0, 4)

			if from, to := args.String("from", ""), args.String("to", ""); from != "" || to != "" {
				if from == "" || to == "" {
					return "", fmt.Errorf("from and to have to be given together")
				}
				pairs = append(pairs, map[string]string{"from": from, "to": to})
			}

			renames, err := args.Object("renames")
			if err != nil {
				return "", err
			}
			for from, to := range renames {
				text, ok := to.(string)
				if !ok {
					return "", fmt.Errorf("renames[%q] has to be a string", from)
				}
				pairs = append(pairs, map[string]string{"from": from, "to": text})
			}

			if len(pairs) == 0 {
				return "", fmt.Errorf("give from and to, or a renames object")
			}
			return t.call(ctx, args, http.MethodPut, "/files/rename", nil, map[string]interface{}{
				"root":  args.String("root", "/"),
				"files": pairs,
			})
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_files_copy",
		Description: "Copy one file or directory in place. The panel names the copy itself, adding a " +
			"suffix; it does not take a destination. To put a copy elsewhere, copy then rename.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("location", "Absolute path of the file or directory to copy.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodPost, "/files/copy", nil, map[string]string{
				"location": args.String("location", ""),
			})
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_files_chmod",
		Description: "Change the permission bits of files or directories. Modes are octal without a " +
			"leading zero, e.g. '644' for a config or '755' for a start script.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("root", "Directory the names are relative to.").Def("/"),
			mcp.Str("file", "Name relative to root, for a single file."),
			mcp.Str("mode", "Octal mode for that file, e.g. '755'."),
			mcp.Obj("modes", "For several at once: an object of name to octal mode."),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			entries := make([]map[string]string, 0, 4)

			if name, mode := args.String("file", ""), args.String("mode", ""); name != "" || mode != "" {
				if name == "" || mode == "" {
					return "", fmt.Errorf("file and mode have to be given together")
				}
				entries = append(entries, map[string]string{"file": name, "mode": mode})
			}

			modes, err := args.Object("modes")
			if err != nil {
				return "", err
			}
			for name, mode := range modes {
				entries = append(entries, map[string]string{"file": name, "mode": fmt.Sprintf("%v", mode)})
			}

			if len(entries) == 0 {
				return "", fmt.Errorf("give file and mode, or a modes object")
			}
			return t.call(ctx, args, http.MethodPost, "/files/chmod", nil, map[string]interface{}{
				"root":  args.String("root", "/"),
				"files": entries,
			})
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_files_compress",
		Description: "Archive files and directories into a new tar.gz, which the panel names and " +
			"places in root. The reply's 'name' is the archive that was created. Useful as a cheap " +
			"snapshot before editing configs, without using a backup slot.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("root", "Directory the names are relative to, and where the archive is written.").Def("/"),
			mcp.StrList("files", "Names to include, relative to root.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			files := args.StringList("files")
			if len(files) == 0 {
				return "", fmt.Errorf("give at least one file or directory to compress")
			}
			return t.call(ctx, args, http.MethodPost, "/files/compress", nil, map[string]interface{}{
				"root":  args.String("root", "/"),
				"files": files,
			})
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_files_pull",
		Description: "Have the node download a URL straight into a server directory, without the " +
			"bytes passing through here. This is how to install a plugin or a jar from a release " +
			"page. The node fetches the URL, so it has to be reachable from the node.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("url", "URL for the node to fetch.").Req(),
			mcp.Str("directory", "Directory to save into.").Def("/"),
			mcp.Str("filename", "Name to save as. Omit to let the node take it from the URL."),
			mcp.Bool("use_header", "Take the filename from the response's Content-Disposition header.").Def(false),
			mcp.Bool("foreground", "Wait for the download to finish instead of queueing it.").Def(false),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			body := map[string]interface{}{
				"url":        args.String("url", ""),
				"directory":  args.String("directory", "/"),
				"use_header": args.Bool("use_header", false),
				"foreground": args.Bool("foreground", false),
			}
			if name := args.String("filename", ""); name != "" {
				body["filename"] = name
			}
			return t.call(ctx, args, http.MethodPost, "/files/pull", nil, body)
		},
	})

	t.add(s, tierDestroy, mcp.Tool{
		Name: "ptero_files_decompress",
		Description: "Extract an archive in place. What it writes is decided by the archive, so a " +
			"file it happens to contain replaces the one already there with no chance to copy it first.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("root", "Directory to extract into.").Def("/"),
			mcp.Str("file", "Archive name, relative to root.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodPost, "/files/decompress", nil, map[string]string{
				"root": args.String("root", "/"),
				"file": args.String("file", ""),
			})
		},
	})

	t.add(s, tierDestroy, mcp.Tool{
		Name: "ptero_files_delete",
		Description: "Delete files and directories. A directory goes with everything inside it. " +
			"Nothing is kept and there is no recycle bin on the panel side.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("root", "Directory the names are relative to.").Def("/"),
			mcp.StrList("files", "Names to delete, relative to root.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			files := args.StringList("files")
			if len(files) == 0 {
				return "", fmt.Errorf("give at least one file or directory to delete")
			}
			for _, name := range files {
				// A root delete would empty the server in one call, which no
				// caller means and which the panel will happily carry out.
				if trimmed := strings.Trim(strings.TrimSpace(name), "/."); trimmed == "" {
					return "", fmt.Errorf("refused: %q would delete the whole server directory; "+
						"name the files or folders to remove", name)
				}
			}
			result, err := t.call(ctx, args, http.MethodPost, "/files/delete", nil, map[string]interface{}{
				"root":  args.String("root", "/"),
				"files": files,
			})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("deleted %d item(s) from %s\n%s",
				len(files), args.String("root", "/"), result), nil
		},
	})
}

// renderListing prints a directory as a compact table.
//
// The panel's own records are ten fields per file, most of them repeated
// across every row. For a folder of two hundred plugins that is most of the
// reply spent on field names, so the table is the default and the full
// records are behind as_json.
func renderListing(directory string, entries []pteroapi.FileEntry) string {
	if len(entries) == 0 {
		return fmt.Sprintf("%s is empty\n", directory)
	}

	var out strings.Builder
	files, dirs := 0, 0
	for _, entry := range entries {
		if entry.IsDirectory {
			dirs++
		} else {
			files++
		}
	}
	fmt.Fprintf(&out, "%s — %d directories, %d files\n", directory, dirs, files)
	fmt.Fprintf(&out, "%-4s %-6s %10s  %-20s %s\n", "type", "mode", "size", "modified", "name")

	for _, entry := range entries {
		kind := "file"
		size := formatSize(entry.Size)
		if entry.IsDirectory {
			kind = "dir"
			size = "-"
		}
		if entry.IsSymlink {
			kind = "link"
		}
		modified := entry.ModifiedAt
		if len(modified) > 19 {
			modified = modified[:19] // seconds are enough; drop the zone
		}
		fmt.Fprintf(&out, "%-4s %-6s %10s  %-20s %s\n", kind, entry.Mode, size, modified, entry.Name)
	}
	return out.String()
}

// renderFile prints a file, applying the line window and the byte ceiling.
func renderFile(path, content string, startLine, lineCount, maxBytes int) string {
	var header strings.Builder
	body := content

	if startLine > 0 || lineCount > 0 {
		lines := strings.Split(content, "\n")
		from := startLine - 1
		if from < 0 {
			from = 0
		}
		if from > len(lines) {
			from = len(lines)
		}
		to := len(lines)
		if lineCount > 0 && from+lineCount < to {
			to = from + lineCount
		}
		body = strings.Join(lines[from:to], "\n")
		fmt.Fprintf(&header, "%s — lines %d-%d of %d\n", path, from+1, to, len(lines))
	} else {
		fmt.Fprintf(&header, "%s — %s, %d lines\n", path, formatSize(int64(len(content))),
			strings.Count(content, "\n")+1)
	}

	if maxBytes > 0 && len(body) > maxBytes {
		body = body[:maxBytes]
		fmt.Fprintf(&header, "truncated at %d bytes; use start_line and line_count to read further\n", maxBytes)
	}

	return header.String() + "---\n" + body
}

// renderSearch prints search hits one per line, with the content preview when
// there is one.
func renderSearch(result *pteroapi.SearchResult) string {
	var out strings.Builder

	fmt.Fprintf(&out, "%d match(es) — read %d directories", len(result.Hits), result.DirectoriesRead)
	if result.FilesScanned > 0 {
		fmt.Fprintf(&out, ", scanned %d files", result.FilesScanned)
	}
	out.WriteString("\n")

	var notes []string
	if result.Truncated {
		notes = append(notes, "result limit reached; raise max_results for more")
	}
	if result.DepthLimitHit {
		notes = append(notes, "depth limit reached; raise max_depth to go deeper")
	}
	if result.SkippedTooLarge > 0 {
		notes = append(notes, fmt.Sprintf("%d file(s) skipped as too large; raise max_file_kb",
			result.SkippedTooLarge))
	}
	if result.SkippedUnreadable > 0 {
		notes = append(notes, fmt.Sprintf("%d file(s) could not be read", result.SkippedUnreadable))
	}
	for _, note := range notes {
		fmt.Fprintf(&out, "note: %s\n", note)
	}

	if len(result.Hits) == 0 {
		return out.String()
	}

	out.WriteString("---\n")
	for _, hit := range result.Hits {
		switch {
		case hit.IsDir:
			fmt.Fprintf(&out, "%s/\n", hit.Path)
		case hit.Line > 0:
			fmt.Fprintf(&out, "%s:%d  %s\n", hit.Path, hit.Line, hit.Preview)
		default:
			fmt.Fprintf(&out, "%s  (%s)\n", hit.Path, formatSize(hit.Size))
		}
	}
	return out.String()
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}
