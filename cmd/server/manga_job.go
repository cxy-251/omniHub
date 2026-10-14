package main

import (
	"errors"
	"os"

	mangajob "omnihub/features/manga"
)

// Business workers bypass HTTP/configuration startup; the parent task manager
// already holds the resource reservation and owns their process group.
func runMangaCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "--manga-pack":
		if len(args) != 2 {
			return true, errors.New("usage: --manga-pack output.cbz")
		}
		return true, mangajob.Run(args[1], os.Stdout, os.Stderr)
	case "--manga-index":
		if len(args) != 1 {
			return true, errors.New("usage: --manga-index")
		}
		return true, mangajob.RunIndex(os.Stdout)
	case "--publish-cbz":
		if len(args) != 3 {
			return true, errors.New("usage: --publish-cbz owned-temporary output.cbz")
		}
		return true, mangajob.Publish(args[1], args[2])
	}
	return false, nil
}
