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
	deriveGroup   bool
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
	// --key, --context and --name are global: every command that names a
	// stream derives it the same way.
	tokenDeriveCmd.Flags().BoolVar(&deriveGroup, "group", false,
		"print the run's group token, which lists every stream of the run rather than reading one")

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
	key := derivationKey()

	derive, context := token.Derive, derivedContext()
	if deriveGroup {
		// A group spans the run, so it drops the job and the name.
		derive, context = token.DeriveGroup, derivedGroupContext()
	}

	tok, err := derive(key, context)
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

// derivationKey is the secret both ends share.
func derivationKey() string {
	return firstEnvOr(deriveKey, "LOG_STREAMER_STREAM_KEY")
}

// derivedGroupContext names the run a group indexes. It stops at the run
// attempt, so every job and every leg lands in the same listing.
func derivedGroupContext() string {
	if deriveContext != "" {
		return deriveContext
	}
	return token.Context(
		os.Getenv("GITHUB_REPOSITORY"),
		os.Getenv("GITHUB_RUN_ID"),
		os.Getenv("GITHUB_RUN_ATTEMPT"),
	)
}

// derivedContext names the stream, falling back to the identity GitHub Actions
// gives a job.
func derivedContext() string {
	name := firstEnvOr(deriveName, "LOG_STREAMER_NAME")
	if deriveContext != "" {
		return token.Context(deriveContext, name)
	}
	return token.Context(
		os.Getenv("GITHUB_REPOSITORY"),
		os.Getenv("GITHUB_RUN_ID"),
		os.Getenv("GITHUB_RUN_ATTEMPT"),
		os.Getenv("GITHUB_JOB"),
		name,
	)
}

// firstEnvOr prefers what the caller passed on the command line.
func firstEnvOr(flag string, names ...string) string {
	if flag != "" {
		return flag
	}
	return firstEnv(names...)
}
