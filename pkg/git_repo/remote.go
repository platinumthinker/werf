package git_repo

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/flant/werf/pkg/lock"
	"github.com/flant/werf/pkg/logger"
	ini "gopkg.in/ini.v1"
	uuid "gopkg.in/satori/go.uuid.v1"
	git "gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
)

const RemoteGitRepoCacheVersion = 4

type Remote struct {
	Base
	Url       string
	ClonePath string // TODO: move CacheVersion & path construction here
	IsDryRun  bool
}

func (repo *Remote) RemoteOriginUrl() (string, error) {
	return repo.remoteOriginUrl(repo.ClonePath)
}

func (repo *Remote) FindCommitIdByMessage(regex string) (string, error) {
	head, err := repo.HeadCommit()
	if err != nil {
		return "", fmt.Errorf("error getting head commit: %s", err)
	}
	return repo.findCommitIdByMessage(repo.ClonePath, regex, head)
}

func (repo *Remote) IsEmpty() (bool, error) {
	return repo.isEmpty(repo.ClonePath)
}

func (repo *Remote) CloneAndFetch() error {
	isCloned, err := repo.Clone()
	if err != nil {
		return err
	}
	if isCloned {
		return nil
	}

	return repo.Fetch()
}

func (repo *Remote) isCloneExists() (bool, error) {
	_, err := os.Stat(repo.ClonePath)
	if err == nil {
		return true, nil
	}

	if !os.IsNotExist(err) {
		return false, fmt.Errorf("cannot clone git repo: %s", err)
	}

	return false, nil
}

func (repo *Remote) Clone() (bool, error) {
	if repo.IsDryRun {
		return false, nil
	}

	var err error

	exists, err := repo.isCloneExists()
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	return true, repo.withRemoteRepoLock(func() error {
		exists, err := repo.isCloneExists()
		if err != nil {
			return err
		}
		if exists {
			return nil
		}

		logger.LogInfoF("Clone %s\n", repo.Url)

		path := filepath.Join("/tmp", fmt.Sprintf("werf-git-repo-%s", uuid.NewV4().String()))

		_, err = git.PlainClone(path, true, &git.CloneOptions{
			URL:               repo.Url,
			RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
		})
		if err != nil {
			return err
		}

		defer os.RemoveAll(path)

		err = os.MkdirAll(filepath.Dir(repo.ClonePath), 0755)
		if err != nil {
			return err
		}

		err = os.Rename(path, repo.ClonePath)
		if err != nil {
			return err
		}

		return nil
	})
}

func (repo *Remote) Fetch() error {
	if repo.IsDryRun {
		return nil
	}

	cfgPath := filepath.Join(repo.ClonePath, "config")

	cfg, err := ini.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("cannot load repo `%s` config: %s", repo.String(), err)
	}

	remoteName := "origin"

	oldUrlKey := cfg.Section(fmt.Sprintf("remote \"%s\"", remoteName)).Key("url")
	if oldUrlKey != nil && oldUrlKey.Value() != repo.Url {
		oldUrlKey.SetValue(repo.Url)
		err := cfg.SaveTo(cfgPath)
		if err != nil {
			return fmt.Errorf("cannot update url of repo `%s`: %s", repo.String(), err)
		}
	}

	return repo.withRemoteRepoLock(func() error {
		rawRepo, err := git.PlainOpen(repo.ClonePath)
		if err != nil {
			return fmt.Errorf("cannot open repo: %s", err)
		}

		logger.LogInfoF("Fetch remote %s of %s\n", remoteName, repo.Url)

		err = rawRepo.Fetch(&git.FetchOptions{RemoteName: remoteName, Force: true})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			return fmt.Errorf("cannot fetch remote `%s` of repo `%s`: %s", remoteName, repo.String(), err)
		}

		return nil
	})
}

func (repo *Remote) HeadCommit() (string, error) {
	repoPath := repo.ClonePath

	repository, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("cannot open repo `%s`: %s", repoPath, err)
	}

	branch, err := repo.HeadBranchName()
	if err != nil {
		return "", fmt.Errorf("cannot detect head branch name of repo `%s`: %s", repoPath, err)
	}

	refName := plumbing.ReferenceName(fmt.Sprintf("refs/remotes/origin/%s", branch))

	ref, err := repository.Reference(refName, true)
	if err != nil {
		return "", fmt.Errorf("cannot resolve reference `%s` of repo `%s`: %s", refName, repoPath, err)
	}

	return ref.Hash().String(), nil
}

func (repo *Remote) HeadBranchName() (string, error) {
	return repo.getHeadBranchName(repo.ClonePath)
}

func (repo *Remote) findReference(rawRepo *git.Repository, reference string) (string, error) {
	refs, err := rawRepo.References()
	if err != nil {
		return "", err
	}

	var res string

	err = refs.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().String() == reference {
			res = fmt.Sprintf("%s", ref.Hash())
			return storer.ErrStop
		}

		return nil
	})
	if err != nil {
		return "", err
	}

	return res, nil
}

func (repo *Remote) LatestBranchCommit(branch string) (string, error) {
	var err error

	rawRepo, err := git.PlainOpen(repo.ClonePath)
	if err != nil {
		return "", fmt.Errorf("cannot open repo: %s", err)
	}

	res, err := repo.findReference(rawRepo, fmt.Sprintf("refs/remotes/origin/%s", branch))
	if err != nil {
		return "", err
	}
	if res == "" {
		return "", fmt.Errorf("unknown branch `%s` of repo `%s`", branch, repo.String())
	}

	fmt.Printf("Using commit `%s` of repo `%s` branch `%s`\n", res, repo.String(), branch)

	return res, nil
}

func (repo *Remote) LatestTagCommit(tag string) (string, error) {
	var err error

	rawRepo, err := git.PlainOpen(repo.ClonePath)
	if err != nil {
		return "", fmt.Errorf("cannot open repo: %s", err)
	}

	res, err := repo.findReference(rawRepo, fmt.Sprintf("refs/tags/%s", tag))
	if err != nil {
		return "", err
	}
	if res == "" {
		return "", fmt.Errorf("unknown tag `%s` of repo `%s`", tag, repo.String())
	}

	fmt.Printf("Using commit `%s` of repo `%s` tag `%s`\n", res, repo.String(), tag)

	return res, nil
}

func (repo *Remote) CreatePatch(opts PatchOptions) (Patch, error) {
	workTreeDir, err := repo.getWorkTreeDir()
	if err != nil {
		return nil, err
	}
	return repo.createPatch(repo.ClonePath, repo.ClonePath, workTreeDir, opts)
}

func (repo *Remote) CreateArchive(opts ArchiveOptions) (Archive, error) {
	workTreeDir, err := repo.getWorkTreeDir()
	if err != nil {
		return nil, err
	}
	return repo.createArchive(repo.ClonePath, repo.ClonePath, workTreeDir, opts)
}

func (repo *Remote) Checksum(opts ChecksumOptions) (Checksum, error) {
	workTreeDir, err := repo.getWorkTreeDir()
	if err != nil {
		return nil, err
	}
	return repo.checksum(repo.ClonePath, repo.ClonePath, workTreeDir, opts)
}

func (repo *Remote) IsCommitExists(commit string) (bool, error) {
	return repo.isCommitExists(repo.ClonePath, commit)
}

func (repo *Remote) getWorkTreeDir() (string, error) {
	ep, err := transport.NewEndpoint(repo.Url)
	if err != nil {
		return "", fmt.Errorf("bad endpoint url `%s`: %s", repo.Url, err)
	}

	return filepath.Join(GetBaseWorkTreeDir(), "remote", ep.Host, ep.Path), nil
}

func (repo *Remote) withRemoteRepoLock(f func() error) error {
	lockName := fmt.Sprintf("remote_git_path.%s", repo.Name)
	return lock.WithLock(lockName, lock.LockOptions{Timeout: 600 * time.Second}, f)
}

func (repo *Remote) TagsList() ([]string, error) {
	return repo.tagsList(repo.ClonePath)
}

func (repo *Remote) RemoteBranchesList() ([]string, error) {
	return repo.remoteBranchesList(repo.ClonePath)
}
