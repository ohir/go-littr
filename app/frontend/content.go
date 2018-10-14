package frontend

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/mariusor/littr.go/app"

	"github.com/juju/errors"
	"github.com/mariusor/littr.go/app/models"
	log "github.com/sirupsen/logrus"

	"github.com/go-chi/chi"
)

const Yay = "yay"
const Nay = "nay"

type comments []*comment
type comment struct {
	models.Item
	Level    uint8
	Children comments
	Parent   *comment
}

type contentModel struct {
	Title         string
	InvertedTheme bool
	Content       comment
}

func loadComments(items []models.Item) comments {
	var comments = make([]*comment, len(items))
	for k, item := range items {
		com := comment{Item: item}
		comments[k] = &com
	}
	return comments
}

func (c comments) getItems() models.ItemCollection {
	var items = make(models.ItemCollection, len(c))
	for k, com := range c {
		items[k] = com.Item
	}
	return items
}

func (c comments) getItemsHashes() []string {
	var items = make([]string, len(c))
	for k, com := range c {
		items[k] = com.Item.Hash.String()
	}
	return items
}

func sluggify(s string) string {
	if s == "" {
		return s
	}
	return strings.Replace(s, "/", "-", -1)
}

func AddLevelComments(comments comments) {
	for _, cur := range comments {
		if len(cur.Children) > 0 {
			for _, child := range cur.Children {
				child.Level = cur.Level + 1
				AddLevelComments(cur.Children)
			}
		}
	}
}

func ReparentComments(allComments []*comment) {
	parFn := func(t []*comment, cur comment) *comment {
		for _, n := range t {
			if cur.Item.Parent != nil && cur.Item.Parent.Hash == n.Hash {
				return n
			}
		}
		return nil
	}

	for _, cur := range allComments {
		if par := parFn(allComments, *cur); par != nil {
			par.Children = append(par.Children, cur)
		}
	}
}

// ShowItem serves /{year}/{month}/{day}/{hash} request
// ShowItem serves /~{handle}/{hash} request
func ShowItem(w http.ResponseWriter, r *http.Request) {
	items := make([]models.Item, 0)
	ShowItemData = true

	m := contentModel{InvertedTheme: isInverted(r)}
	itemLoader, ok := models.ContextItemLoader(r.Context())
	if !ok {
		Logger.WithFields(log.Fields{}).Errorf("could not load item repository from Context")
		return
	}
	handle := chi.URLParam(r, "handle")
	//acctLoader, ok := val.(models.CanLoadAccounts)
	//if !ok {
	//	Logger.WithFields(log.Fields{}).Errorf("could not load account repository from Context")
	//}
	//act, err := acctLoader.LoadAccount(models.LoadAccountFilter{Handle: handle})

	hash := chi.URLParam(r, "hash")
	i, err := itemLoader.LoadItem(models.LoadItemsFilter{
		AttributedTo: []models.Hash{models.Hash(handle)},
		Key:          []string{hash},
	})
	if err != nil {
		Logger.WithFields(log.Fields{}).Error(err)
		HandleError(w, r, http.StatusNotFound, err)
		return
	}
	m.Content = comment{Item: i /*Path: i.Path, FullPath: i.FullPath*/}
	if i.Data == "" {
		HandleError(w, r, http.StatusNotFound, errors.Errorf("not found"))
		return
	}
	items = append(items, i)
	allComments := make(comments, 1)
	allComments[0] = &m.Content

	contentItems, err := itemLoader.LoadItems(models.LoadItemsFilter{
		Context:  []string{m.Content.Hash.String()},
		MaxItems: MaxContentItems,
	})
	if err != nil {
		Logger.WithFields(log.Fields{}).Error(err)
		HandleError(w, r, http.StatusNotFound, err)
		return
	}
	allComments = append(allComments, loadComments(contentItems)...)

	ReparentComments(allComments)
	AddLevelComments(allComments)

	acc, ok := models.ContextCurrentAccount(r.Context())
	if ok && acc.IsLogged() {
		votesLoader, ok := models.ContextVoteLoader(r.Context())
		if ok {
			acc.Votes, err = votesLoader.LoadVotes(models.LoadVotesFilter{
				AttributedTo: []models.Hash{acc.Hash},
				ItemKey:      allComments.getItemsHashes(),
				MaxItems:     MaxContentItems,
			})
			if err != nil {
				Logger.WithFields(log.Fields{}).Error(err)
			}
		} else {
			Logger.WithFields(log.Fields{}).Errorf("could not load vote repository from Context")
		}
	}
	if len(m.Title) > 0 {
		m.Title = fmt.Sprintf("%s", i.Title)
	} else {
		// FIXME(marius): we lost the handle of the account
		m.Title = fmt.Sprintf("%s comment", genitive(m.Content.SubmittedBy.Handle))
	}
	RenderTemplate(r, w, "content", m)
}

func genitive(name string) string {
	l := len(name)
	if l == 0 {
		return name
	}
	if name[l-1:l] != "s" {
		return name + "'s"
	}
	return name + "'"
}

// HandleVoting serves /{year}/{month}/{day}/{hash}/{direction} request
// HandleVoting serves /~{handle}/{direction} request
func HandleVoting(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")

	val := r.Context().Value(models.RepositoryCtxtKey)
	itemLoader, ok := val.(models.CanLoadItems)
	if !ok {
		Logger.WithFields(log.Fields{}).Errorf("could not load item repository from Context")
		return
	}

	p, err := itemLoader.LoadItem(models.LoadItemsFilter{Key: []string{hash}})
	if err != nil {
		Logger.WithFields(log.Fields{}).Error(err)
		HandleError(w, r, http.StatusNotFound, err)
		return
	}

	multiplier := 0
	switch chi.URLParam(r, "direction") {
	case Yay:
		multiplier = 1
	case Nay:
		multiplier = -1
	}
	url := ItemPermaLink(p)

	acc, ok := models.ContextCurrentAccount(r.Context())
	if acc.IsLogged() {
		if auth, ok := val.(models.Authenticated); ok {
			auth.WithAccount(acc)
		}
		voter, ok := val.(models.CanSaveVotes)
		backUrl := r.Header.Get("Referer")
		if !strings.Contains(backUrl, url) && strings.Contains(backUrl, app.Instance.BaseURL) {
			url = fmt.Sprintf("%s#item-%s", backUrl, p.Hash)
		}
		if !ok {
			Logger.WithFields(log.Fields{}).Errorf("could not load vote repository from Context")
			return
		}
		v := models.Vote{
			SubmittedBy: acc,
			Item:        &p,
			Weight:      multiplier * models.ScoreMultiplier,
		}
		if _, err := voter.SaveVote(v); err != nil {
			Logger.WithFields(log.Fields{
				"hash":   v.Item.Hash,
				"author": v.SubmittedBy.Handle,
				"weight": v.Weight,
			}).Error(err)
		}
	} else {
		addFlashMessage(Error, fmt.Sprintf("unable to add vote as an %s user", acc.Handle), r)
	}
	Redirect(w, r, url, http.StatusFound)
}
