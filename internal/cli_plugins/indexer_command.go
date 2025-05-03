package cliplugins

import (
	"context"
	"fmt"
	"fs/internal/indexer"
	"os"

	"github.com/spf13/cobra"
)

type IndexerCommand struct {
	cmd     *cobra.Command
	indexer *indexer.Indexer
}

func NewIndexerCommand(indexer *indexer.Indexer) *IndexerCommand {
	return &IndexerCommand{indexer: indexer}
}

func (i *IndexerCommand) Meta() *cobra.Command {
	if i.cmd != nil {
		return i.cmd
	}
	i.cmd = &cobra.Command{
		Use:   "Indexer",
		Short: "Indexes file changes",
		Long:  "Indexes file changes...",
		Annotations: map[string]string{
			cobra.BashCompOneRequiredFlag: "true",
		},
	}
	i.cmd.Flags().StringP("path", "p", "", "file path")
	i.cmd.Flags().BoolP("file", "f", false, "index the file")
	i.cmd.Flags().BoolP("dir", "d", false, "index the dir")
	i.cmd.Flags().BoolP("remove", "r", false, "remove file index")
	return i.cmd
}

func (i *IndexerCommand) Execute(ctx context.Context, cmd *cobra.Command, args []string) error {
	path, err := cmd.Flags().GetString("path")
	if err != nil || path == "" {
		return fmt.Errorf("flag --path is required")
	}

	isFile, err := cmd.Flags().GetBool("file")
	if err != nil {
		return fmt.Errorf("flag --file failed")
	}

	isDir, err := cmd.Flags().GetBool("dir")
	if err != nil {
		return fmt.Errorf("flag --dir failed")
	}

	isRemove, err := cmd.Flags().GetBool("remove")
	if err != nil {
		return fmt.Errorf("floag --remove failde")
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("path does not exist: %s", path)
		}
		return fmt.Errorf("error when getting information about the path: %w", err)
	}

	if isFile && info.IsDir() {
		if err := i.indexer.IndexFile(path); err != nil {
			return fmt.Errorf("failed index file: %s, err: %w", path, err)
		}
		return nil
	}

	if isDir && info.Mode().IsRegular() {
		if err := i.indexer.IndexDirectory(path); err != nil {
			return fmt.Errorf("failed index dir: %s, err: %w", path, err)
		}
		return nil
	}

	if isRemove && info.IsDir() {
		if err := i.indexer.RemoveFileIndex(path); err != nil {
			return fmt.Errorf("failed to remove file index: %s, err: %w", path, err)
		}
		return nil
	}

	return nil
}
