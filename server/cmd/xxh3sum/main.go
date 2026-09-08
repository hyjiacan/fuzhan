package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"fuzhan/internal/resources"

	"github.com/zeebo/xxh3"
)

// reorderArgs moves all flag arguments before positional arguments,
// so flags like -g, -b, -h work regardless of their position.
// Go's flag.Parse() stops at the first non-flag argument.
func reorderArgs() {
	args := os.Args[1:]

	// Check for missing value after -o before reordering
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" && (i+1 >= len(args) || strings.HasPrefix(args[i+1], "-")) {
			fmt.Fprintln(os.Stderr, "Error: -o requires a file path argument")
			os.Exit(1)
		}
	}

	var flags, positional []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			// -b and -o consume the next argument as their value
			if (args[i] == "-b" || args[i] == "-o") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, args[i])
		}
	}
	os.Args = append([]string{os.Args[0]}, append(flags, positional...)...)
}

func printHelp() {
	fmt.Print(resources.Usage)
	os.Exit(0)
}

func hashFile(filename string, bitLen int) string {
	file, err := os.Open(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	if bitLen == 64 {
		hash := xxh3.Hash(data)
		return fmt.Sprintf("%016x", hash)
	}
	h128 := xxh3.Hash128(data)
	return fmt.Sprintf("%016x%016x", h128.Hi, h128.Lo)
}

func collectFiles(args []string) []string {
	var files []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error accessing %s: %v\n", arg, err)
			continue
		}
		if info.IsDir() {
			entries, err := os.ReadDir(arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading directory %s: %v\n", arg, err)
				continue
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					files = append(files, filepath.Join(arg, entry.Name()))
				}
			}
		} else {
			files = append(files, arg)
		}
	}
	return files
}

func main() {
	reorderArgs()
	bitLen := flag.Int("b", 64, "hash bit length: 64 or 128")
	group := flag.Bool("d", false, "group redundant files with identical hashes")
	output := flag.String("o", "", "output file path")
	help := flag.Bool("h", false, "show help")
	flag.Parse()

	if *help {
		printHelp()
	}

	// Check if -o was explicitly set but with an empty value
	outputSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "o" {
			outputSet = true
		}
	})
	if outputSet && *output == "" {
		fmt.Fprintln(os.Stderr, "Error: -o requires a non-empty file path")
		os.Exit(1)
	}

	// resultOut writes to both console and file when -o is specified
	var resultOut io.Writer = os.Stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		resultOut = io.MultiWriter(os.Stdout, f)
	}

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: xxh3sum [-b 64|128] [-d] <file_or_dir...>")
		os.Exit(1)
	}

	if *bitLen != 64 && *bitLen != 128 {
		fmt.Fprintln(os.Stderr, "Error: bit length must be 64 or 128")
		os.Exit(1)
	}

	files := collectFiles(args)
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "No files found")
		os.Exit(1)
	}

	hashMap := make(map[string][]string)

	for _, f := range files {
		h := hashFile(f, *bitLen)
		hashMap[h] = append(hashMap[h], f)
		fmt.Fprintf(resultOut, "%s  %s\n", h, f)
	}

	if *group {
		hasDupes := false
		for h, fList := range hashMap {
			if len(fList) > 1 {
				hasDupes = true
				fmt.Fprintf(resultOut, "\n[Duplicate] %s (%d files)\n", h, len(fList))
				for _, f := range fList {
					fmt.Fprintf(resultOut, "  %s\n", f)
				}
			}
		}
		if !hasDupes {
			fmt.Fprintln(resultOut, "\nNo duplicate files found.")
		}
	}

	if *output != "" {
		fmt.Printf("Output written to: %s\n", *output)
	}
}
