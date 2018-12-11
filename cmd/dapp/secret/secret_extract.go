package secret

import (
	"bytes"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/spf13/cobra"

	"github.com/flant/dapp/cmd/dapp/common"
	"github.com/flant/dapp/pkg/deploy/secret"
)

var ExtractCmdData struct {
	FilePath       string
	OutputFilePath string
	Values         bool
}

func NewExtractCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extract",
		Short: "Extract data",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := runSecretExtract()
			if err != nil {
				return fmt.Errorf("secret extract failed: %s", err)
			}
			return nil
		},
	}

	common.SetupDir(&CommonCmdData, cmd)
	common.SetupTmpDir(&CommonCmdData, cmd)
	common.SetupHomeDir(&CommonCmdData, cmd)

	cmd.PersistentFlags().StringVarP(&ExtractCmdData.FilePath, "file-path", "", "", "Decode file data by specified path")
	cmd.PersistentFlags().StringVarP(&ExtractCmdData.OutputFilePath, "output-file-path", "", "", "Save decoded data by specified file path")
	cmd.PersistentFlags().BoolVarP(&ExtractCmdData.Values, "values", "", false, "Decode specified FILE_PATH (--file-path) as secret values file")

	return cmd
}

func runSecretExtract() error {
	projectDir, err := common.GetProjectDir(&CommonCmdData)
	if err != nil {
		return fmt.Errorf("getting project dir failed: %s", err)
	}

	options := &secretGenerateOptions{
		FilePath:       ExtractCmdData.FilePath,
		OutputFilePath: ExtractCmdData.OutputFilePath,
		Values:         ExtractCmdData.Values,
	}

	m, err := secret.GetManager(projectDir)
	if err != nil {
		return err
	}

	return secretExtract(m, options)
}

func secretExtract(m secret.Manager, options *secretGenerateOptions) error {
	var encodedData []byte
	var data []byte
	var err error

	if options.FilePath != "" {
		encodedData, err = readFileData(options.FilePath)
		if err != nil {
			return err
		}
	} else {
		encodedData, err = readStdin()
		if err != nil {
			return err
		}

		if len(encodedData) == 0 {
			return nil
		}
	}

	encodedData = bytes.TrimSpace(encodedData)

	if options.FilePath != "" && options.Values {
		data, err = m.ExtractYamlData(encodedData)
		if err != nil {
			return err
		}
	} else {
		data, err = m.Extract(encodedData)
		if err != nil {
			return err
		}
	}

	if options.OutputFilePath != "" {
		if err := saveGeneratedData(options.OutputFilePath, data); err != nil {
			return err
		}
	} else {
		if terminal.IsTerminal(int(os.Stdout.Fd())) {
			if !bytes.HasSuffix(data, []byte("\n")) {
				data = append(data, []byte("\n")...)
			}
		}

		fmt.Printf(string(data))
	}

	return nil
}