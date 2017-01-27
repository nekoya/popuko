package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"strings"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"

	"github.com/karen-irc/popuko/epic"
	"github.com/karen-irc/popuko/input"
	"github.com/karen-irc/popuko/queue"
	"github.com/karen-irc/popuko/setting"
)

// AppServer is just an this application.
type AppServer struct {
	githubClient  *github.Client
	autoMergeRepo *queue.AutoMergeQRepo
	setting       *setting.Settings
}

const prefixWebHookPath = "/github"

func (srv *AppServer) handleGithubHook(rw http.ResponseWriter, req *http.Request) {
	log.Println("info: Start: handle GitHub WebHook")
	log.Printf("info: Path is %v\n", req.URL.Path)
	defer log.Println("info End: handle GitHub WebHook")

	if req.Method != "POST" {
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	payload, err := github.ValidatePayload(req, config.WebHookSecret())
	if err != nil {
		rw.WriteHeader(http.StatusPreconditionFailed)
		io.WriteString(rw, err.Error())
		return
	}

	event, err := github.ParseWebHook(github.WebHookType(req), payload)
	if err != nil {
		rw.WriteHeader(http.StatusPreconditionFailed)
		io.WriteString(rw, err.Error())
		return
	}

	switch event := event.(type) {
	case *github.IssueCommentEvent:
		ok, err := srv.processIssueCommentEvent(event)
		rw.WriteHeader(http.StatusOK)
		if ok {
			io.WriteString(rw, "result: \n")
		}

		if err != nil {
			log.Printf("info: %v\n", err)
			io.WriteString(rw, err.Error())
		}
		return
	case *github.PushEvent:
		srv.processPushEvent(event)
		rw.WriteHeader(http.StatusOK)
		return
	case *github.StatusEvent:
		srv.processStatusEvent(event)
		rw.WriteHeader(http.StatusOK)
		return
	default:
		rw.WriteHeader(http.StatusOK)
		log.Println("warn: Unsupported type events")
		log.Println(reflect.TypeOf(event))
		io.WriteString(rw, "This event type is not supported: "+github.WebHookType(req))
		return
	}
}

func (srv *AppServer) processIssueCommentEvent(ev *github.IssueCommentEvent) (bool, error) {
	log.Printf("Start: processCommitCommentEvent by %v\n", *ev.Comment.ID)
	defer log.Printf("End: processCommitCommentEvent by %v\n", *ev.Comment.ID)

	if action := ev.Action; (action == nil) || (*action != "created") {
		return false, fmt.Errorf("info: accept `action === \"created\"` only")
	}

	repoOwner := *ev.Repo.Owner.Login
	repo := *ev.Repo.Name
	if !srv.setting.AcceptRepo(repoOwner, repo) {
		n := repoOwner + "/" + repo
		log.Printf("======= error: =======\n This event is from an unaccepted repository: %v\n==============", n)
		return false, fmt.Errorf("%v is not accepted", n)
	}

	body := *ev.Comment.Body
	ok, cmd := input.ParseCommand(body)
	if !ok {
		return false, fmt.Errorf("No operations which this bot should handle.")
	}

	if cmd == nil {
		return false, fmt.Errorf("error: unexpected result of parsing comment body")
	}

	repoInfo := epic.GetRepositoryInfo(srv.githubClient.Repositories, repoOwner, repo)
	if repoInfo == nil {
		return false, fmt.Errorf("debug: cannot get repositoryInfo")
	}

	switch cmd := cmd.(type) {
	case *input.AssignReviewerCommand:
		return epic.AssignReviewer(srv.githubClient, ev, cmd.Reviewer)
	case *input.AcceptChangeByReviewerCommand:
		commander := epic.AcceptCommand{
			repoOwner,
			repo,
			srv.githubClient,
			config.BotNameForGithub(),
			cmd,
			repoInfo,
			srv.autoMergeRepo,
		}
		return commander.AcceptChangesetByReviewer(ev)
	case *input.AcceptChangeByOthersCommand:
		commander := epic.AcceptCommand{
			repoOwner,
			repo,
			srv.githubClient,
			config.BotNameForGithub(),
			cmd,
			repoInfo,
			srv.autoMergeRepo,
		}
		return commander.AcceptChangesetByReviewer(ev)
	case *input.CancelApprovedByReviewerCommand:
		commander := epic.CancelApprovedCommand{
			BotName:       config.BotNameForGithub(),
			Client:        srv.githubClient,
			Owner:         repoOwner,
			Name:          repo,
			Number:        *ev.Issue.Number,
			Cmd:           cmd,
			Info:          repoInfo,
			AutoMergeRepo: srv.autoMergeRepo,
		}
		return commander.CancelApprovedChangeSet(ev)
	default:
		return false, fmt.Errorf("error: unreachable")
	}
}

func (srv *AppServer) processPushEvent(ev *github.PushEvent) {
	log.Println("info: Start: processPushEvent by push id")
	defer log.Println("info: End: processPushEvent by push id")

	repoOwner := *ev.Repo.Owner.Name
	log.Printf("debug: repository owner is %v\n", repoOwner)
	repo := *ev.Repo.Name
	log.Printf("debug: repository name is %v\n", repo)
	if !srv.setting.AcceptRepo(repoOwner, repo) {
		n := repoOwner + "/" + repo
		log.Printf("======= error: =======\n This event is from an unaccepted repository: %v\n==============", n)
		return
	}

	epic.DetectUnmergeablePR(srv.githubClient, ev)
}

func (srv *AppServer) processStatusEvent(ev *github.StatusEvent) {
	log.Println("info: Start: processStatusEvent")
	defer log.Println("info: End: processStatusEvent")

	repoOwner := *ev.Repo.Owner.Login
	log.Printf("debug: repository owner is %v\n", repoOwner)
	repo := *ev.Repo.Name
	log.Printf("debug: repository name is %v\n", repo)
	if !srv.setting.AcceptRepo(repoOwner, repo) {
		n := repoOwner + "/" + repo
		log.Printf("======= error: =======\n This event is from an unaccepted repository: %v\n==============", n)
		return
	}

	epic.CheckAutoBranch(srv.githubClient, srv.autoMergeRepo, ev)
}

func createGithubClient(config *setting.Settings) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{
			AccessToken: config.GithubToken(),
		},
	)
	tc := oauth2.NewClient(oauth2.NoContext, ts)
	client := github.NewClient(tc)
	return client
}

const prefixRestAPI = "/api/v0"
const prefixQueueInfoAPI = "/queue/"

func (srv *AppServer) handleRESTApiRequest(rw http.ResponseWriter, req *http.Request) {
	p := strings.TrimPrefix(req.URL.Path, prefixRestAPI)
	if strings.HasPrefix(p, prefixQueueInfoAPI) {
		repo := strings.TrimPrefix(p, prefixQueueInfoAPI)
		srv.getQueueInfoForRepository(rw, req, repo)
		return
	}

	rw.WriteHeader(http.StatusNotFound)
}

func (srv *AppServer) getQueueInfoForRepository(rw http.ResponseWriter, req *http.Request, repo string) {
	if req.Method != "GET" {
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var owner string
	var name string
	{
		tmp := strings.Split(repo, "/")
		if !(len(tmp) == 2) && !(len(tmp) == 3) { // accept `/bar/foo/` style.
			rw.WriteHeader(http.StatusNotFound)
			m := "info: the repo name is invalid"
			log.Printf(m+"%+v\n", tmp)
			io.WriteString(rw, m)
			return
		}

		owner = tmp[0]
		name = tmp[1]
	}

	qhandle := srv.autoMergeRepo.Get(owner, name)
	if qhandle == nil {
		rw.WriteHeader(http.StatusNotFound)
		m := fmt.Sprintf("error: cannot get the queue handle for `%v/%v`", owner, name)
		log.Println(m)
		io.WriteString(rw, m)
		return
	}

	qhandle.Lock()
	defer qhandle.Unlock()

	b := qhandle.LoadAsRawByte()
	if b == nil {
		rw.WriteHeader(http.StatusInternalServerError)
		m := fmt.Sprintf("error: cannot get the queue information for `%v/%v`", owner, name)
		log.Println(m)
		io.WriteString(rw, m)
		return
	}

	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.WriteHeader(http.StatusOK)
	rw.Write(b)
}
