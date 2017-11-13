package main

import (
	"errors"
	"fmt"
	"github.com/coreos/go-semver/semver"
	"github.com/google/go-github/github"
	version "github.com/hashicorp/go-version"
	gitconfig "github.com/tcnksm/go-gitconfig"
	"io/ioutil"
	"os"
	"strings"

	"context"
	"encoding/xml"
	"flag"
	"golang.org/x/oauth2"
	"path/filepath"
	"sort"
)

type Project struct {
	Version string `xml:"version"`
}

type config struct {
	dryrun bool
	debug  bool
	dir    string
	owner  string
	repo   string
}

func main() {

	var c config
	c.dryrun = *flag.Bool("dryrun", false, "avoids pushing Git tag for new release")
	c.debug = *flag.Bool("debug", false, "prints debug into to console")
	c.dir = *flag.String("folder", "", "the folder to look for files that contain the project version to bump")
	c.owner = *flag.String("org", "", "the git repository owner e.g. fabric8io")
	c.repo = *flag.String("repo", "", "the git repository e.g. fabric8")

	v, err := getNewVersionFromTag(c)
	if err != nil {
		fmt.Errorf("failed to get new version", err)
		os.Exit(-1)
	}
	fmt.Print(fmt.Sprintf("%s", v))
}

func getVersion(c config) (string, error) {

	m, err := ioutil.ReadFile(c.dir + string(filepath.Separator) + "Makefile")
	if err == nil {
		str := string(m)
		if c.debug {
			fmt.Println("Found Makefile")
		}
		if strings.Contains(str, "VERSION") {
			parts := strings.Split(str, "=")

			v := strings.TrimSpace(parts[1])
			if v != "" {
				if c.debug {
					fmt.Println(fmt.Sprintf("existing Makefile version %v"), v)
				}
				return v, nil
			}
		}
	}

	p, err := ioutil.ReadFile(c.dir + string(filepath.Separator) + "pom.xml")
	if err == nil {
		if c.debug {
			fmt.Println("Found pom.xml")
		}
		var project Project
		xml.Unmarshal(p, &project)
		if project.Version != "" {
			fmt.Println(fmt.Sprintf("Existing version %v", project.Version))
			return project.Version, nil
		}
	}
	return "", errors.New("no recognised file to obtain current version from")
}

func getLatestGithubTag(c config) (string, error) {

	// if repo isn't provided by flags fall back to using current repo if run from a git project
	var owner string
	var repo string
	if c.owner != "" {
		owner = c.owner
	} else {
		o, err := gitconfig.Repository()
		if err != nil {
			return "", fmt.Errorf("no git repo flag provided and not executed in a git repo with an origin URL: %v", err)
		}
		owner = o
	}
	if c.repo != "" {
		repo = c.repo
	} else {
		r, err := gitconfig.Repository()
		if err != nil {
			return "", fmt.Errorf("no git repo flag provided and not executed in a git repo with an origin URL: %v", err)
		}
		repo = r
	}

	token := os.Getenv("GITHUB_AUTH_TOKEN")
	ctx := context.Background()
	var client *github.Client
	if token != "" {

		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
		tc := oauth2.NewClient(ctx, ts)

		client = github.NewClient(tc)
	} else {
		if c.debug {
			fmt.Println("No GITHUB_AUTH_TOKEN env var found so using unauthenticated request")
		}
		client = github.NewClient(nil)
	}

	tags, _, err := client.Repositories.ListTags(ctx, owner, repo, nil)

	if err != nil {
		return "", err
	}
	if len(tags) == 0 {
		return "", errors.New(fmt.Sprintf("No tags found for %s/%s", owner, repo))
	}

	// build an array of all the tags
	versionsRaw := make([]string, len(tags))
	for i, tag := range tags {
		if c.debug {
			fmt.Println(fmt.Sprintf("found tag %s", tag.GetName()))
		}
		versionsRaw[i] = tag.GetName()
	}

	// turn the array into a new collection of versions that we can sort
	versions := make([]*version.Version, len(versionsRaw))
	for i, raw := range versionsRaw {
		v, _ := version.NewVersion(raw)
		versions[i] = v
	}

	// return the highest existing tag
	sort.Sort(version.Collection(versions))
	latest := len(versions)
	return versions[latest-1].String(), nil
}

func getNewVersionFromTag(c config) (string, error) {

	// get the latest github tag
	tag, err := getLatestGithubTag(c)
	if err != nil {
		// use a default if no existing version found
		return "", err
	}
	sv, err := semver.NewVersion(tag)
	if err != nil {
		// use a default if no existing version found
		return "1.0.0", nil
	}

	sv.BumpPatch()

	majorVersion := sv.Major
	minorVersion := sv.Minor
	patchVersion := sv.Patch

	// check if major or minor version has been changed
	baseVersion, err := getVersion(c)
	if err != nil {
		return fmt.Sprintf("%d.%d.%d", majorVersion, minorVersion, patchVersion), nil
	}

	// turn into semver
	bsv, err := semver.NewVersion(baseVersion)
	if err != nil {
		return "", err
	}
	baseMajorVersion := bsv.Major
	baseMinorVersion := bsv.Minor
	basePatchVersion := bsv.Patch

	if baseMajorVersion > majorVersion ||
		(baseMajorVersion == majorVersion &&
			(baseMinorVersion > minorVersion) || (baseMinorVersion == minorVersion && basePatchVersion > patchVersion)) {
		majorVersion = baseMajorVersion
		minorVersion = baseMinorVersion
		patchVersion = basePatchVersion
	}
	return fmt.Sprintf("%d.%d.%d", majorVersion, minorVersion, patchVersion), nil
}
