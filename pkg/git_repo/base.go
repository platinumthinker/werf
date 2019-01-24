package git_repo

import (
	"crypto/md5"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar"
	"github.com/flant/werf/pkg/lock"
	"github.com/flant/werf/pkg/logger"
	"github.com/flant/werf/pkg/true_git"
	git "gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

var (
	errNotABranch = errors.New("cannot get branch name: HEAD refers to a specific revision that is not associated with a branch name")
)

type Base struct {
	Name   string
	TmpDir string
}

func (repo *Base) HeadCommit() (string, error) {
	panic("not implemented")
}

func (repo *Base) LatestBranchCommit(branch string) (string, error) {
	panic("not implemented")
}

func (repo *Base) LatestTagCommit(branch string) (string, error) {
	panic("not implemented")
}

func (repo *Base) remoteOriginUrl(repoPath string) (string, error) {
	repository, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("cannot open repo `%s`: %s", repoPath, err)
	}

	cfg, err := repository.Config()
	if err != nil {
		return "", fmt.Errorf("cannot access repo config: %s", err)
	}

	if originCfg, hasKey := cfg.Remotes["origin"]; hasKey {
		return originCfg.URLs[0], nil
	}

	return "", nil
}

func (repo *Base) findCommitIdByMessage(repoPath string, regex string, headCommit string) (string, error) {
	repository, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("cannot open repo `%s`: %s", repoPath, err)
	}

	headHash, err := newHash(headCommit)
	if err != nil {
		return "", fmt.Errorf("bad head commit hash `%s`: %s", headCommit, err)
	}

	commitObj, err := repository.CommitObject(headHash)
	if err != nil {
		return "", err
	}

	commitIter := object.NewCommitIterBSF(commitObj, nil, nil)

	regexObj, err := regexp.Compile(regex)
	if err != nil {
		return "", fmt.Errorf("bad regex `%s`: %s", regex, err)
	}

	var foundCommit *object.Commit

	err = commitIter.ForEach(func(c *object.Commit) error {
		if regexObj.Match([]byte(c.Message)) {
			foundCommit = c
			return storer.ErrStop
		}

		return nil
	})

	if foundCommit != nil {
		return foundCommit.Hash.String(), nil
	}
	return "", nil
}

func (repo *Base) isEmpty(repoPath string) (bool, error) {
	repository, err := git.PlainOpen(repoPath)
	if err != nil {
		return false, fmt.Errorf("cannot open repo `%s`: %s", repoPath, err)
	}

	commitIter, err := repository.CommitObjects()
	if err != nil {
		return false, err
	}

	_, err = commitIter.Next()
	if err == io.EOF {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func (repo *Base) withWorkTreeLock(workTree string, f func() error) error {
	lockName := fmt.Sprintf("git_work_tree %s", workTree)
	return lock.WithLock(lockName, lock.LockOptions{Timeout: 600 * time.Second}, f)
}

func (repo *Base) getReferenceForRepo(repoPath string) (*plumbing.Reference, error) {
	var err error

	repository, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open repo `%s`: %s", repoPath, err)
	}

	return repository.Head()
}

func (repo *Base) getHeadBranchName(repoPath string) (string, error) {
	ref, err := repo.getReferenceForRepo(repoPath)
	if err != nil {
		return "", fmt.Errorf("cannot get repo `%s` head: %s", repoPath, err)
	}

	if ref.Name().IsBranch() {
		branchRef := ref.Name()
		return strings.Split(string(branchRef), "refs/heads/")[1], nil
	}

	return "", errNotABranch
}

func (repo *Base) String() string {
	return repo.GetName()
}

func (repo *Base) GetName() string {
	return repo.Name
}

func (repo *Base) createPatch(repoPath, gitDir, workTreeDir string, opts PatchOptions) (Patch, error) {
	repository, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open repo `%s`: %s", repoPath, err)
	}

	fromHash, err := newHash(opts.FromCommit)
	if err != nil {
		return nil, fmt.Errorf("bad `from` commit hash `%s`: %s", opts.FromCommit, err)
	}

	_, err = repository.CommitObject(fromHash)
	if err != nil {
		return nil, fmt.Errorf("bad `from` commit `%s`: %s", opts.FromCommit, err)
	}

	toHash, err := newHash(opts.ToCommit)
	if err != nil {
		return nil, fmt.Errorf("bad `to` commit hash `%s`: %s", opts.ToCommit, err)
	}

	toCommit, err := repository.CommitObject(toHash)
	if err != nil {
		return nil, fmt.Errorf("bad `to` commit `%s`: %s", opts.ToCommit, err)
	}

	hasSubmodules, err := HasSubmodulesInCommit(toCommit)
	if err != nil {
		return nil, err
	}

	patch := NewTmpPatchFile()

	fileHandler, err := os.OpenFile(patch.GetFilePath(), os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return nil, fmt.Errorf("cannot open patch file `%s`: %s", patch.GetFilePath(), err)
	}

	patchOpts := true_git.PatchOptions{
		FromCommit: opts.FromCommit,
		ToCommit:   opts.ToCommit,
		PathFilter: true_git.PathFilter{
			BasePath:     opts.BasePath,
			IncludePaths: opts.IncludePaths,
			ExcludePaths: opts.ExcludePaths,
		},
		WithEntireFileContext: opts.WithEntireFileContext,
		WithBinary:            opts.WithBinary,
	}

	var desc *true_git.PatchDescriptor

	if hasSubmodules {
		err = repo.withWorkTreeLock(workTreeDir, func() error {
			desc, err = true_git.PatchWithSubmodules(fileHandler, gitDir, workTreeDir, patchOpts)
			return err
		})
	} else {
		desc, err = true_git.Patch(fileHandler, gitDir, patchOpts)
	}

	if err != nil {
		return nil, fmt.Errorf("error creating patch between `%s` and `%s` commits: %s", opts.FromCommit, opts.ToCommit, err)
	}

	patch.Descriptor = desc

	err = fileHandler.Close()
	if err != nil {
		return nil, fmt.Errorf("error creating patch file `%s`: %s", patch.GetFilePath(), err)
	}

	return patch, nil
}

func HasSubmodulesInCommit(commit *object.Commit) (bool, error) {
	_, err := commit.File(".gitmodules")
	if err == object.ErrFileNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (repo *Base) createArchive(repoPath, gitDir, workTreeDir string, opts ArchiveOptions) (Archive, error) {
	logger.LogInfoF("Using work tree %s\n", workTreeDir)

	repository, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open repo `%s`: %s", repoPath, err)
	}

	commitHash, err := newHash(opts.Commit)
	if err != nil {
		return nil, fmt.Errorf("bad commit hash `%s`: %s", opts.Commit, err)
	}

	commit, err := repository.CommitObject(commitHash)
	if err != nil {
		return nil, fmt.Errorf("bad commit `%s`: %s", opts.Commit, err)
	}

	hasSubmodules, err := HasSubmodulesInCommit(commit)
	if err != nil {
		return nil, err
	}

	archive := NewTmpArchiveFile()

	fileHandler, err := os.OpenFile(archive.GetFilePath(), os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return nil, fmt.Errorf("cannot open archive file: %s", err)
	}

	archiveOpts := true_git.ArchiveOptions{
		Commit: opts.Commit,
		PathFilter: true_git.PathFilter{
			BasePath:     opts.BasePath,
			IncludePaths: opts.IncludePaths,
			ExcludePaths: opts.ExcludePaths,
		},
	}

	var desc *true_git.ArchiveDescriptor

	if hasSubmodules {
		err = repo.withWorkTreeLock(workTreeDir, func() error {
			desc, err = true_git.ArchiveWithSubmodules(fileHandler, gitDir, workTreeDir, archiveOpts)
			return err
		})
	} else {
		err = repo.withWorkTreeLock(workTreeDir, func() error {
			desc, err = true_git.Archive(fileHandler, gitDir, workTreeDir, archiveOpts)
			return err
		})
	}

	if err != nil {
		return nil, fmt.Errorf("error creating archive for commit `%s`: %s", opts.Commit, err)
	}

	archive.Descriptor = desc

	return archive, nil
}

func (repo *Base) isCommitExists(repoPath string, commit string) (bool, error) {
	repository, err := git.PlainOpen(repoPath)
	if err != nil {
		return false, fmt.Errorf("cannot open repo `%s`: %s", repoPath, err)
	}

	commitHash, err := newHash(commit)
	if err != nil {
		return false, fmt.Errorf("bad commit hash `%s`: %s", commit, err)
	}

	_, err = repository.CommitObject(commitHash)
	if err == plumbing.ErrObjectNotFound {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("bad commit `%s`: %s", commit, err)
	}
	return true, nil
}

func (repo *Base) tagsList(repoPath string) ([]string, error) {
	repository, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open repo `%s`: %s", repoPath, err)
	}

	tags, err := repository.TagObjects()
	if err != nil {
		return nil, err
	}

	res := make([]string, 0)
	err = tags.ForEach(func(t *object.Tag) error {
		res = append(res, t.Name)
		return nil
	})

	return res, nil
}

func (repo *Base) remoteBranchesList(repoPath string) ([]string, error) {
	repository, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open repo `%s`: %s", repoPath, err)
	}

	branches, err := repository.References()
	if err != nil {
		return nil, err
	}

	remoteBranchPrefix := "refs/remotes/origin/"

	res := make([]string, 0)
	err = branches.ForEach(func(r *plumbing.Reference) error {
		refName := r.Name().String()
		if strings.HasPrefix(refName, remoteBranchPrefix) {
			value := strings.TrimPrefix(refName, remoteBranchPrefix)
			if value != "HEAD" {
				res = append(res, value)
			}
		}
		return nil
	})

	return res, nil
}

func (repo *Base) checksum(repoPath, gitDir, workTreeDir string, opts ChecksumOptions) (Checksum, error) {
	repository, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open repo `%s`: %s", repoPath, err)
	}

	commitHash, err := newHash(opts.Commit)
	if err != nil {
		return nil, fmt.Errorf("bad commit hash `%s`: %s", opts.Commit, err)
	}

	commit, err := repository.CommitObject(commitHash)
	if err != nil {
		return nil, fmt.Errorf("bad commit `%s`: %s", opts.Commit, err)
	}

	hasSubmodules, err := HasSubmodulesInCommit(commit)
	if err != nil {
		return nil, err
	}

	checksum := &ChecksumDescriptor{
		NoMatchPaths: make([]string, 0),
		Hash:         sha256.New(),
	}

	err = repo.withWorkTreeLock(workTreeDir, func() error {
		if hasSubmodules {
			err := true_git.PrepareWorkTreeWithSubmodules(gitDir, workTreeDir, opts.Commit)
			if err != nil {
				return err
			}
		} else {
			err := true_git.PrepareWorkTree(gitDir, workTreeDir, opts.Commit)
			if err != nil {
				return err
			}
		}

		paths := make([]string, 0)

		for _, pathPattern := range opts.Paths {
			res, err := getFilesByPattern(workTreeDir, filepath.Join(opts.BasePath, pathPattern))
			if err != nil {
				return fmt.Errorf("error getting files by path pattern `%s`: %s", pathPattern, err)
			}

			if len(res) == 0 {
				checksum.NoMatchPaths = append(checksum.NoMatchPaths, pathPattern)
				if debugChecksum() {
					fmt.Printf("Ignore checksum path pattern `%s`: no matches found\n", pathPattern)
				}
			}

			paths = append(paths, res...)
		}

		sort.Strings(paths)

		pathFilter := true_git.PathFilter{
			BasePath:     opts.BasePath,
			IncludePaths: opts.IncludePaths,
			ExcludePaths: opts.ExcludePaths,
		}

		for _, path := range paths {
			fullPath := filepath.Join(workTreeDir, path)

			if !pathFilter.IsFilePathValid(path) {
				if debugChecksum() {
					fmt.Printf("Excluded file `%s` from resulting checksum by path filter %s\n", fullPath, pathFilter.String())
				}
				continue
			}

			_, err = checksum.Hash.Write([]byte(path))
			if err != nil {
				return fmt.Errorf("error calculating checksum of path `%s`: %s", path, err)
			}

			stat, err := os.Lstat(fullPath)
			// file should exist after being scanned
			if err != nil {
				return fmt.Errorf("error accessing file `%s`: %s", fullPath, err)
			}

			_, err = checksum.Hash.Write([]byte(fmt.Sprintf("%o", stat.Mode())))
			if err != nil {
				return fmt.Errorf("error calculating checksum of file `%s` mode: %s", fullPath, err)
			}

			if stat.Mode().IsRegular() {
				f, err := os.Open(fullPath)
				if err != nil {
					return fmt.Errorf("unable to open file `%s`: %s", fullPath, err)
				}

				_, err = io.Copy(checksum.Hash, f)
				if err != nil {
					return fmt.Errorf("error calculating checksum of file `%s` content: %s", fullPath, err)
				}

				err = f.Close()
				if err != nil {
					return fmt.Errorf("error closing file `%s`: %s", fullPath, err)
				}

				if debugChecksum() {
					f, err := os.Open(fullPath)
					if err != nil {
						return fmt.Errorf("unable to open file `%s`: %s", fullPath, err)
					}

					hash := md5.New()
					_, err = io.Copy(hash, f)
					if err != nil {
						return fmt.Errorf("error reading file `%s` content: %s", fullPath, err)
					}
					contentHash := fmt.Sprintf("%x", hash.Sum(nil))

					err = f.Close()
					if err != nil {
						return fmt.Errorf("error closing file `%s`: %s", fullPath, err)
					}

					fmt.Printf("Added file `%s` to resulting checksum with content checksum: %s\n", fullPath, contentHash)
				}
			} else if stat.Mode()&os.ModeSymlink != 0 {
				linkname, err := os.Readlink(fullPath)
				if err != nil {
					return fmt.Errorf("cannot read symlink `%s`: %s", fullPath, err)
				}

				_, err = checksum.Hash.Write([]byte(linkname))
				if err != nil {
					return fmt.Errorf("error calculating checksum of symlink `%s`: %s", fullPath, err)
				}

				if debugChecksum() {
					fmt.Printf("Added symlink `%s` -> `%s` to resulting checksum\n", fullPath, linkname)
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if debugChecksum() {
		fmt.Printf("Calculated checksum %s\n", checksum.String())
	}

	return checksum, nil
}

func getFilesByPattern(baseDir, pathPattern string) ([]string, error) {
	fullPathPattern := filepath.Join(baseDir, pathPattern)

	matches, err := doublestar.Glob(fullPathPattern)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0)

	for _, fullPath := range matches {
		stat, err := os.Lstat(fullPath)
		// file should exist after glob match
		if err != nil {
			return nil, fmt.Errorf("error accessing file `%s`: %s", fullPath, err)
		}

		if stat.Mode().IsRegular() || (stat.Mode()&os.ModeSymlink != 0) {
			path, err := filepath.Rel(baseDir, fullPath)
			if err != nil {
				return nil, fmt.Errorf("error getting relative path for `%s`: %s", fullPath, err)
			}

			paths = append(paths, path)
		} else if stat.Mode().IsDir() {
			err := filepath.Walk(fullPath, func(fullWalkPath string, info os.FileInfo, accessErr error) error {
				if accessErr != nil {
					return fmt.Errorf("error accessing file `%s`: %s", fullWalkPath, err)
				}

				if info.Mode().IsRegular() || (stat.Mode()&os.ModeSymlink != 0) {
					path, err := filepath.Rel(baseDir, fullWalkPath)
					if err != nil {
						return fmt.Errorf("error getting relative path for `%s`: %s", fullWalkPath, err)
					}

					paths = append(paths, path)
				}

				return nil
			})

			if err != nil {
				return nil, fmt.Errorf("error scanning directory `%s`: %s", fullPath, err)
			}
		}
	}

	return paths, nil
}

func debugChecksum() bool {
	return os.Getenv("WERF_DEBUG_GIT_REPO_CHECKSUM") == "1"
}
