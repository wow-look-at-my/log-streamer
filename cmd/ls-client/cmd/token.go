package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/log-streamer/internal/token"
)

var (
	deriveKey     string
	deriveContext string
	deriveName    string
)

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Generate or derive a stream token",
}

var tokenGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Print a fresh random stream token",
	Args:  cobra.NoArgs,
	RunE:  runTokenGenerate,
}

var tokenDeriveCmd = &cobra.Command{
	Use:   "derive",
	Short: "Print the stream token for a build, derived from a shared key",
	Long: `Print the stream token for a build, derived from a shared key.

The token is HMAC-SHA256 of a context string under a key, so a writer and a
reader who share the key compute the same token without exchanging it. This is
what makes a CI build watchable while it runs: a server-minted token reaches
the job over its own socket, and the job's only channel back is its log, which
the CI provider hides until the run ends.

Inside GitHub Actions the context defaults to the repository, run id, run
attempt and job. A reader has all of those from the API, which serves run
metadata immediately even while the log is still withheld.

Matrix legs of a job share GITHUB_JOB, so give each leg a --name (its matrix
values, say) or every leg writes into the same stream.`,
	Args: cobra.NoArgs,
	RunE: runTokenDerive,
}

func init() {
	tokenDeriveCmd.Flags().StringVar(&deriveKey, "key", "",
		"shared derivation key (overrides LOG_STREAMER_STREAM_KEY env)")
	tokenDeriveCmd.Flags().StringVar(&deriveContext, "context", "",
		"context to derive from (default: repository/run-id/run-attempt/job from the GitHub Actions env)")
	tokenDeriveCmd.Flags().StringVar(&deriveName, "name", "",
		"extra context appended to the default, to separate streams within a job")

	tokenCmd.AddCommand(tokenGenerateCmd)
	tokenCmd.AddCommand(tokenDeriveCmd)
	rootCmd.AddCommand(tokenCmd)
}

func runTokenGenerate(cmd *cobra.Command, args []string) error {
	tok, err := token.Generate()
	if err != nil {
		return err
	}
	fmt.Println(tok)
	return nil
}

func runTokenDerive(cmd *cobra.Command, args []string) error {
	key := deriveKey
	if key == "" {
		key = os.Getenv("LOG_STREAMER_STREAM_KEY")
	}

	tok, err := token.Derive(key, deriveContextOrDefault())
	switch {
	case err == token.ErrNoKey:
		return fmt.Errorf("no derivation key: pass --key or set LOG_STREAMER_STREAM_KEY")
	case err == token.ErrNoContext:
		return fmt.Errorf("no derivation context: pass --context, or run where GITHUB_REPOSITORY and GITHUB_RUN_ID are set")
	case err != nil:
		return err
	}

	fmt.Println(tok)
	return nil
}

// deriveContextOrDefault names the stream, falling back to the identity
// GitHub Actions gives a job.
func deriveContextOrDefault() string {
	if deriveContext != "" {
		return token.Context(deriveContext, deriveName)
	}
	return token.Context(
		os.Getenv("GITHUB_REPOSITORY"),
		os.Getenv("GITHUB_RUN_ID"),
		os.Getenv("GITHUB_RUN_ATTEMPT"),
		os.Getenv("GITHUB_JOB"),
		deriveName,
	)
}
