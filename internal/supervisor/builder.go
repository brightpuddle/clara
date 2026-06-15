package supervisor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/brightpuddle/clara/internal/loghub"
)

// resolveRepoRoot finds the Clara module root using the following strategy (in order):
//  1. The explicitly supplied repoRoot argument (non-empty string).
//  2. The CLARA_REPO_ROOT environment variable.
//  3. Walking up from the running executable's directory.
//  4. Walking up from the current working directory.
//
// Returns an error only if none of the strategies locate a go.mod.
func resolveRepoRoot(explicit string) (string, error) {
	// 1. Explicit override.
	if explicit != "" {
		if _, err := os.Stat(filepath.Join(explicit, "go.mod")); err == nil {
			return explicit, nil
		}
	}

	// 2. Environment variable.
	if env := os.Getenv("CLARA_REPO_ROOT"); env != "" {
		if _, err := os.Stat(filepath.Join(env, "go.mod")); err == nil {
			return env, nil
		}
	}

	// 3. Walk up from the running executable.
	if exe, err := os.Executable(); err == nil {
		if root, err := findModuleRoot(filepath.Dir(exe)); err == nil {
			return root, nil
		}
	}

	// 4. Walk up from the current working directory.
	if cwd, err := os.Getwd(); err == nil {
		if root, err := findModuleRoot(cwd); err == nil {
			return root, nil
		}
	}

	return "", errors.New(
		"cannot locate clara module root: set CLARA_REPO_ROOT or run from within the source tree",
	)
}

// CompileResult carries the outcome of a sandboxed compilation attempt.
type CompileResult struct {
	Success       bool   `json:"success"`
	BinaryPath    string `json:"binary_path,omitempty"`
	CompilerError string `json:"compiler_error,omitempty"`
}

// Builder manages the sandboxed compilation workspace for self-modifying Actuator plugins.
type Builder struct {
	baseDir  string // base workspace directory, e.g. ~/.local/share/clara/workspace/
	repoRoot string // Clara module root (for go.mod replace directive)
	hub      *loghub.Hub
}

// NewBuilder creates a new Builder with a verified workspace directory.
// repoRoot is the path to the Clara source tree (go.mod lives there). If empty,
// resolveRepoRoot will locate it automatically via CLARA_REPO_ROOT env var or
// by walking up from the running binary / current working directory.
func NewBuilder(baseDir, repoRoot string) (*Builder, error) {
	if len(baseDir) > 0 && baseDir[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get user home directory")
		}
		baseDir = filepath.Join(home, baseDir[1:])
	}

	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, errors.Wrap(err, "failed to create builder base directory")
	}

	// Initialize git repository in workspace directory if not present.
	gitDir := filepath.Join(baseDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		initCmd := exec.Command("git", "init", "-b", "main")
		initCmd.Dir = baseDir
		if err := initCmd.Run(); err != nil {
			initCmd2 := exec.Command("git", "init")
			initCmd2.Dir = baseDir
			_ = initCmd2.Run()
		}
	}

	// Ensure there is at least one commit so we have a valid HEAD.
	runGitLocal := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = baseDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Clara Builder",
			"GIT_AUTHOR_EMAIL=clara@brightpuddle.com",
			"GIT_COMMITTER_NAME=Clara Builder",
			"GIT_COMMITTER_EMAIL=clara@brightpuddle.com",
		)
		return cmd.CombinedOutput()
	}

	if _, err := runGitLocal("rev-parse", "--verify", "HEAD"); err != nil {
		_, _ = runGitLocal("commit", "--allow-empty", "-m", "initial commit")
	}

	root, err := resolveRepoRoot(repoRoot)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve clara module root")
	}

	return &Builder{baseDir: baseDir, repoRoot: root}, nil
}

// WithHub sets the log hub for the builder.
func (b *Builder) WithHub(hub *loghub.Hub) *Builder {
	b.hub = hub
	return b
}

func (b *Builder) pushEval(level, msg string, fields map[string]any) {
	if b.hub != nil {
		b.hub.PushEvaluator(level, msg, fields)
	}
}

// runGit executes a git command in the Builder base workspace directory.
func (b *Builder) runGit(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = b.baseDir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Clara Builder",
		"GIT_AUTHOR_EMAIL=clara@brightpuddle.com",
		"GIT_COMMITTER_NAME=Clara Builder",
		"GIT_COMMITTER_EMAIL=clara@brightpuddle.com",
	)
	return cmd.CombinedOutput()
}

// CompileAndVerify compiles a proposed Go plugin inside a restricted native workspace.
// It writes the main source file and potential test file, executes `go test`, executes `go build`,
// and returns full compiler diagnostics on failure so the LLM Evaluator can refine its code.
func (b *Builder) CompileAndVerify(
	ctx context.Context,
	actuatorID string,
	codeMap map[string]string, // filename -> file content
) (CompileResult, error) {
	// 1. Get the current main branch name
	var mainBranch string
	if out, err := b.runGit(ctx, "symbolic-ref", "--short", "HEAD"); err == nil {
		mainBranch = strings.TrimSpace(string(out))
	}
	if mainBranch == "" {
		mainBranch = "main" // fallback
	}

	// 2. Checkout a dedicated build branch: build/<actuator_id>-<timestamp>
	timestamp := time.Now().Format("20060102-150405")
	buildBranch := fmt.Sprintf("build/%s-%s", actuatorID, timestamp)
	if out, err := b.runGit(ctx, "checkout", "-b", buildBranch); err != nil {
		return CompileResult{}, errors.Wrapf(
			err,
			"failed to checkout build branch: %s",
			string(out),
		)
	}

	success := false
	defer func() {
		if !success {
			// If compilation or test verification fails, check out the main branch to discard the changes.
			_, _ = b.runGit(ctx, "checkout", mainBranch)
			// Discard any untracked or modified files in the workspace
			_, _ = b.runGit(ctx, "clean", "-fd")
			_, _ = b.runGit(ctx, "checkout", "--", ".")
			// Delete the build branch
			_, _ = b.runGit(ctx, "branch", "-D", buildBranch)
		}
	}()

	// 3. Create a unique, isolated workspace subdirectory for this compilation pass.
	subDir := filepath.Join(b.baseDir, fmt.Sprintf("%s_compile", actuatorID))
	if err := os.MkdirAll(subDir, 0o700); err != nil {
		return CompileResult{}, errors.Wrap(err, "failed to create compiler workspace subdirectory")
	}

	// 4. Write all provided source files into the isolated workspace.
	for filename, content := range codeMap {
		filePath := filepath.Join(subDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
			return CompileResult{}, errors.Wrapf(
				err,
				"failed to write source file %s to workspace",
				filename,
			)
		}
	}

	// Make sure a go.mod exists in the subfolder so it behaves as an isolated compilation module.
	// The replace directive points at the local Clara source tree so generated actuators can
	// import pkg/sdk without requiring a published module version.
	goModPath := filepath.Join(subDir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		modContent := fmt.Sprintf(
			"module %s\n\ngo 1.24\n\nrequire github.com/brightpuddle/clara v0.0.0\n\nreplace github.com/brightpuddle/clara => %s\n",
			actuatorID,
			b.repoRoot,
		)
		if err := os.WriteFile(goModPath, []byte(modContent), 0o600); err != nil {
			return CompileResult{}, errors.Wrap(err, "failed to initialize sandbox go.mod")
		}
	}

	// 5. Run `go mod tidy` to populate go.sum and resolve transitive dependencies
	// before any build or test commands are run.
	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = subDir
	tidyCmd.Env = append(os.Environ(), "GO111MODULE=on")

	if tidyOutput, err := tidyCmd.CombinedOutput(); err != nil {
		b.pushEval("error", "builder: go mod tidy failed", map[string]any{
			"actuator":    actuatorID,
			"diagnostics": string(tidyOutput),
		})
		return CompileResult{
			Success:       false,
			CompilerError: fmt.Sprintf("go mod tidy failed:\n%s", string(tidyOutput)),
		}, nil
	}

	// 6. Execute `go test` in the sandbox directory to verify logical correctness.
	testCmd := exec.CommandContext(ctx, "go", "test", "-v", "./...")
	testCmd.Dir = subDir
	testCmd.Env = append(os.Environ(), "GO111MODULE=on")

	testOutput, err := testCmd.CombinedOutput()
	if err != nil {
		b.pushEval("error", "builder: test suite failed", map[string]any{
			"actuator":    actuatorID,
			"diagnostics": string(testOutput),
		})
		return CompileResult{
			Success:       false,
			CompilerError: fmt.Sprintf("Test suite failures:\n%s", string(testOutput)),
		}, nil
	}

	// 7. Compile the native binary using `go build`.
	binaryName := actuatorID
	outputPath := filepath.Join(subDir, binaryName)

	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", binaryName, ".")
	buildCmd.Dir = subDir
	buildCmd.Env = append(os.Environ(), "GO111MODULE=on")

	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		b.pushEval("error", "builder: compilation failed", map[string]any{
			"actuator":    actuatorID,
			"diagnostics": string(buildOutput),
		})
		return CompileResult{
			Success:       false,
			CompilerError: fmt.Sprintf("Compiler errors:\n%s", string(buildOutput)),
		}, nil
	}

	// 8. Success! Move the compiled binary to the final active bin folder.
	finalBinDir := filepath.Join(b.baseDir, "bin")
	if err := os.MkdirAll(finalBinDir, 0o700); err != nil {
		return CompileResult{}, errors.Wrap(err, "failed to create final bin directory")
	}

	finalPath := filepath.Join(finalBinDir, binaryName)
	if err := os.Rename(outputPath, finalPath); err != nil {
		// Try copy if rename fails due to cross-device boundaries.
		if err := copyFile(outputPath, finalPath); err != nil {
			return CompileResult{}, errors.Wrap(err, "failed to move binary to final directory")
		}
	}

	// Change permissions to make it executable.
	if err := os.Chmod(finalPath, 0o700); err != nil {
		return CompileResult{}, errors.Wrap(
			err,
			"failed to set executable permission on actuator binary",
		)
	}

	// Commit successful compile changes to git.
	if out, err := b.runGit(ctx, "add", "."); err != nil {
		return CompileResult{}, errors.Wrapf(err, "failed to git add changes: %s", string(out))
	}

	commitMsg := fmt.Sprintf("Add compiled actuator %s", actuatorID)
	if out, err := b.runGit(ctx, "commit", "-m", commitMsg); err != nil {
		if !strings.Contains(string(out), "nothing to commit") {
			return CompileResult{}, errors.Wrapf(err, "failed to commit changes: %s", string(out))
		}
	}

	// Switch back to the main branch.
	if out, err := b.runGit(ctx, "checkout", mainBranch); err != nil {
		return CompileResult{}, errors.Wrapf(err, "failed to checkout main branch: %s", string(out))
	}

	// Merge build branch back to main.
	if out, err := b.runGit(ctx, "merge", "--no-ff", "-m", fmt.Sprintf("Merge branch '%s' for %s", buildBranch, actuatorID), buildBranch); err != nil {
		_, _ = b.runGit(ctx, "merge", "--abort")
		return CompileResult{}, errors.Wrapf(
			err,
			"failed to merge build branch to main: %s",
			string(out),
		)
	}

	// Tag the merge commit as stable.
	tagOpt := fmt.Sprintf("stable/%s-%s", actuatorID, timestamp)
	if out, err := b.runGit(ctx, "tag", tagOpt); err != nil {
		return CompileResult{}, errors.Wrapf(err, "failed to create stable tag: %s", string(out))
	}

	// Clean up / delete the build branch.
	if _, err := b.runGit(ctx, "branch", "-d", buildBranch); err != nil {
		_, _ = b.runGit(ctx, "branch", "-D", buildBranch)
	}

	success = true

	b.pushEval("info", "builder: actuator compiled", map[string]any{
		"actuator": actuatorID,
		"binary":   finalPath,
	})

	return CompileResult{
		Success:    true,
		BinaryPath: finalPath,
	}, nil
}

// CommitToGit anchors the stable compiled change in the local host git history.
func (b *Builder) CommitToGit(
	ctx context.Context,
	repoPath string,
	actuatorID string,
	commitMsg string,
) error {
	// Validate git repository presence.
	gitDir := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return errors.New("target path is not a git repository")
	}

	// Run git commit.
	addCmd := exec.CommandContext(ctx, "git", "add", ".")
	addCmd.Dir = repoPath
	if err := addCmd.Run(); err != nil {
		return errors.Wrap(err, "failed to git add changes")
	}

	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg)
	commitCmd.Dir = repoPath
	// We allow failure if nothing changed.
	_ = commitCmd.Run()

	return nil
}

// GetLatestStableBinary retrieves the binary content of the last stable tag for an actuator.
func (b *Builder) GetLatestStableBinary(ctx context.Context, actuatorID string) ([]byte, error) {
	// List tags for this actuator
	out, err := b.runGit(ctx, "tag", "--list", fmt.Sprintf("stable/%s-*", actuatorID))
	if err != nil {
		return nil, errors.Wrap(err, "failed to list stable tags")
	}

	tags := strings.Split(strings.TrimSpace(string(out)), "\n")
	var validTags []string
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" {
			validTags = append(validTags, t)
		}
	}

	if len(validTags) == 0 {
		return nil, errors.New("no stable version tag found for this actuator")
	}

	// Sort tags lexicographically to find the latest
	latestTag := validTags[0]
	for _, t := range validTags {
		if t > latestTag {
			latestTag = t
		}
	}

	// Retrieve file content at latestTag:bin/<actuator_id>
	binaryRelPath := fmt.Sprintf("bin/%s", actuatorID)
	showArg := fmt.Sprintf("%s:%s", latestTag, binaryRelPath)
	binData, err := b.runGit(ctx, "show", showArg)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve binary from git tag %s", latestTag)
	}

	return binData, nil
}

// ApplyPatches applies a slice of PatchInstructions to the existing source files in the actuator
// workspace subdirectory.  Each instruction performs an exact, unique search-and-replace:
//   - If the Search block is not found in the file, an error is returned describing which block
//     was missing so the self-healing loop can ask the LLM for a corrected patch.
//   - If the Search block appears more than once, an error is returned to prevent ambiguous edits.
//
// The function does NOT compile; call CompileAndVerify separately after a successful patch.
func (b *Builder) ApplyPatches(
	ctx context.Context,
	actuatorID string,
	patches []PatchInstruction,
) error {
	subDir := filepath.Join(b.baseDir, fmt.Sprintf("%s_compile", actuatorID))

	for i, p := range patches {
		if p.File == "" {
			return errors.Newf("patch[%d]: missing file name", i)
		}
		if p.Search == "" {
			return errors.Newf("patch[%d] (%s): search block must not be empty", i, p.File)
		}

		filePath := filepath.Join(subDir, p.File)
		raw, err := os.ReadFile(filePath)
		if err != nil {
			return errors.Wrapf(err, "patch[%d]: cannot read file %q", i, p.File)
		}

		content := string(raw)
		count := strings.Count(content, p.Search)

		switch count {
		case 0:
			return errors.Newf(
				"patch[%d] (%s): search block not found — check indentation and whitespace\n"+
					">>> search block:\n%s\n<<<",
				i, p.File, p.Search,
			)
		case 1:
			// Exact match: apply the replacement.
			content = strings.Replace(content, p.Search, p.Replace, 1)
		default:
			return errors.Newf(
				"patch[%d] (%s): search block appears %d times (must be unique) — "+
					"provide more surrounding context to disambiguate\n"+
					">>> search block:\n%s\n<<<",
				i, p.File, count, p.Search,
			)
		}

		if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
			return errors.Wrapf(err, "patch[%d]: failed to write patched file %q", i, p.File)
		}

		b.pushEval("debug", "builder: patch applied", map[string]any{
			"actuator": actuatorID,
			"file":     p.File,
			"patch":    i,
		})

		// Yield to the context between patches so a cancellation is respected.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	return nil
}

// ReadActuatorFiles reads all Go source files from the actuator's compile subdirectory and
// returns them as a filename→content map suitable for passing to CompileAndVerify.
func (b *Builder) ReadActuatorFiles(
	_ context.Context,
	actuatorID string,
) (map[string]string, error) {
	subDir := filepath.Join(b.baseDir, fmt.Sprintf("%s_compile", actuatorID))

	entries, err := os.ReadDir(subDir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read actuator workspace for %q", actuatorID)
	}

	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Only include Go source files and go.mod/go.sum; skip binaries and hidden files.
		if !strings.HasSuffix(name, ".go") &&
			name != "go.mod" &&
			name != "go.sum" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(subDir, name))
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read file %q in actuator workspace", name)
		}
		result[name] = string(data)
	}

	return result, nil
}

// findModuleRoot walks up the directory tree from startDir until it finds a go.mod file,
// returning the directory that contains it. This is used to resolve the replace directive
// for the local Clara module in sandbox go.mod files.
func findModuleRoot(startDir string) (string, error) {
	dir := startDir
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found: reached filesystem root")
		}
		dir = parent
	}
}

// Helper function to copy files if cross-device renaming occurs.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
