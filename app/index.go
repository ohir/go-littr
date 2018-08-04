package app

import (
	"bytes"
	"fmt"
	"github.com/juju/errors"
	"github.com/mariusor/littr.go/models"
	log "github.com/sirupsen/logrus"
	"gopkg.in/russross/blackfriday.v2"
	"html/template"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	MaxContentItems = 200
)

type Account struct {
	Id        int64
	Hash      string    `json:"key"`
	Email     []byte    `json:"email"`
	Handle    string    `json:"handle"`
	Score     int64     `json:"score"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	flags     int8
	metadata  []byte
	votes     []Vote
}

type Vote struct {
	SubmittedBy string    `json:"submitted_by"`
	SubmittedAt time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Weight      int       `json:"weight"`
	ItemHash    string    `json:"item_hash"`
	id          int64
	itemId      int64
	flags       int8
}

type Item struct {
	id          int64
	Hash        string    `json:"key"`
	Title       string    `json:"title"`
	MimeType    string    `json:"mime_type"`
	Data        string    `json:"data"`
	Score       int64     `json:"score"`
	SubmittedAt time.Time `json:"created_at"`
	SubmittedBy string    `json:"submitted_by"`
	UpdatedAt   time.Time `json:"updated_at"`
	flags       int8
	path        []byte
	fullPath    []byte
	metadata    []byte
	parentLink  string
	permaLink   string
	opLink      string
	isLink      bool
	isTop       bool
}

func (a *Account) VotedOn(i Item) *Vote {
	for _, v := range a.votes {
		if v.itemId == i.id {
			return &v
		}
	}
	return nil
}

func (v *Vote) IsYay() bool {
	return v != nil && v.Weight > 0
}
func (v *Vote) IsNay() bool {
	return v != nil && v.Weight < 0
}

func (i Item) Deleted() bool {
	return false
}

func (i Item) ScoreFmt() string {
	score := 0.0
	units := ""
	base := float64(i.Score) / models.ScoreMultiplier
	d := math.Ceil(math.Log10(math.Abs(base)))
	if d < 5 {
		score = math.Ceil(base)
		return fmt.Sprintf("%d", int(score))
	} else if d < 8 {
		score = base / models.ScoreMaxK
		units = "K"
	} else if d < 11 {
		score = base / models.ScoreMaxM
		units = "M"
	} else if d < 13 {
		score = base / models.ScoreMaxB
		units = "B"
	} else {
		sign := ""
		if base < 0 {
			sign = "-"
		}
		return fmt.Sprintf("%s%s", sign, "∞")
	}

	return fmt.Sprintf("%3.1f%s", score, units)
}
func (i Item) scoreLink(dir string) string {
	return fmt.Sprintf("%s/%s", i.PermaLink(), dir)
}
func (i Item) ScoreUPLink() string {
	return i.scoreLink("yay")
}
func (i Item) ScoreDOWNLink() string {
	return i.scoreLink("nay")
}
func (i Item) UnDelete() {
	i.flags ^= models.FlagsDeleted
}
func (i *Item) Delete() {
	i.flags &= models.FlagsDeleted
}
func (i Item) IsLink() bool {
	return i.isLink
}
func (i Item) IsTop() bool {
	return i.isTop
}
func (i Item) GetDomain() string {
	if !i.IsLink() {
		return ""
	}
	return strings.Split(i.Data, "/")[2]
}
func (i Item) ISODate() string {
	return i.SubmittedAt.Format("2006-01-02T15:04:05.000-07:00")
}
func (i Item) FromNow() string {
	td := time.Now().Sub(i.SubmittedAt)
	pluralize := func(d float64, unit string) string {
		if math.Round(d) != 1 {
			if unit == "century" {
				unit = "centurie"
			}
			return unit + "s"
		}
		return unit
	}
	val := 0.0
	unit := ""
	when := "ago"

	hours := math.Abs(td.Hours())
	minutes := math.Abs(td.Minutes())
	seconds := math.Abs(td.Seconds())

	if td.Seconds() < 0 {
		// we're in the future
		when = "in the future"
	}
	if seconds < 30 {
		return "now"
	}
	if hours < 1 {
		if minutes < 1 {
			val = math.Mod(seconds, 60)
			unit = "second"
		} else {
			val = math.Mod(minutes, 60)
			unit = "minute"
		}
	} else if hours < 24 {
		val = hours
		unit = "hour"
	} else if hours < 168 {
		val = hours / 24
		unit = "day"
	} else if hours < 672 {
		val = hours / 168
		unit = "week"
	} else if hours < 8760 {
		val = hours / 672
		unit = "month"
	} else if hours < 87600 {
		val = hours / 8760
		unit = "year"
	} else if hours < 876000 {
		val = hours / 87600
		unit = "decade"
	} else {
		val = hours / 876000
		unit = "century"
	}
	return fmt.Sprintf("%.0f %s %s", val, pluralize(val, unit), when)
}

func (i Item) PermaLink() string {
	return fmt.Sprintf("/~%s/%s", i.SubmittedBy, i.Hash)
}

func (i Item) ParentLink() string {
	return i.parentLink
}

func (i Item) OPLink() string {
	if i.opLink != "" {
		return i.opLink
	}
	return "/"
}

func (i Item) IsSelf() bool {
	mimeComponents := strings.Split(i.MimeType, "/")
	return mimeComponents[0] == "text"
}
func (i Item) HTML() template.HTML {
	return template.HTML(string(i.Data))
}
func (i Item) Markdown() template.HTML {
	return template.HTML(blackfriday.Run([]byte(i.Data)))
}
func (i Item) Text() string {
	return string(i.Data)
}

type AccountMetadata struct {
	password string
	salt     string
}

func (i comment) Level() int {
	if i.path == nil {
		return 0
	}
	return bytes.Count(i.path, []byte(".")) + 1
}

type indexModel struct {
	Title         string
	InvertedTheme bool
	Items         comments
	User          *Account
}

func getAuthProviders() map[string]string {
	p := make(map[string]string)
	if os.Getenv("GITHUB_KEY") != "" {
		p["github"] = "Github"
	}
	if os.Getenv("GITLAB_KEY") != "" {
		p["gitlab"] = "Gitlab"
	}
	if os.Getenv("GOOGLE_KEY") != "" {
		p["google"] = "Google"
	}
	if os.Getenv("FACEBOOK_KEY") != "" {
		p["facebook"] = "Facebook"
	}

	return p
}

func LoadVotes(a *Account, it []Item) ([]Vote, error) {
	var ids = make([]int64, len(it))
	var hashes = make(map[int64]string, 0)
	for i, k := range it {
		ids[i] = k.id
		hashes[k.id] = k.Hash
	}
	if a == nil {
		return nil, errors.Errorf("no account to load for")
	}
	if len(ids) == 0 {
		return nil, errors.Errorf("no ids to load")
	}
	// this here code following is the ugliest I wrote in quite a long time
	// so ugly it warrants its own fucking shame corner
	sids := make([]string, 0)
	for i := 0; i < len(ids); i++ {
		sids = append(sids, fmt.Sprintf("$%d", i+2))
	}
	iitems := make([]interface{}, len(ids)+1)
	iitems[0] = a.Id
	for i, v := range ids {
		iitems[i+1] = v
	}
	sel := fmt.Sprintf(`select "id", "submitted_by", "submitted_at", "updated_at", "item_id", "weight", "flags"
	from "votes" where "submitted_by" = $1 and "item_id" in (%s)`, strings.Join(sids, ", "))
	rows, err := Db.Query(sel, iitems...)
	if err != nil {
		return nil, err
	}
	if a.votes == nil {
		a.votes = make([]Vote, 0)
	}
RowLoop:
	for rows.Next() {
		v := models.Vote{}
		err = rows.Scan(&v.Id, &v.SubmittedBy, &v.SubmittedAt, &v.UpdatedAt, &v.ItemId, &v.Weight, &v.Flags)
		if err != nil {
			return nil, err
		}
		for key, vv := range a.votes {
			if vv.id == v.Id {
				a.votes[key] = Vote{
					SubmittedBy: a.Handle,
					SubmittedAt: v.SubmittedAt,
					UpdatedAt:   v.UpdatedAt,
					ItemHash:    hashes[v.Id],
					Weight:      v.Weight,
					id:          v.Id,
					flags:       v.Flags,
					itemId:      v.ItemId,
				}
				continue RowLoop
			}
		}
		a.votes = append(a.votes, Vote{
			SubmittedBy: a.Handle,
			SubmittedAt: v.SubmittedAt,
			UpdatedAt:   v.UpdatedAt,
			ItemHash:    hashes[v.Id],
			Weight:      v.Weight,
			id:          v.Id,
			flags:       v.Flags,
			itemId:      v.ItemId,
		})
	}
	return a.votes, nil
}

func ParentLink(c models.Content) string {
	if len(c.Path) == 0 {
		return "/"
	} else {
		lastDotPos := bytes.LastIndex(c.Path, []byte(".")) + 1
		parentHash := c.Path[lastDotPos : lastDotPos+8]
		return fmt.Sprintf("/parent/%s/%s", c.Hash(), parentHash)
	}
}
func OPLink(c models.Content) string {
	if len(c.Path) > 0 {
		parentHash := c.Path[0:8]
		return fmt.Sprintf("/op/%s/%s", c.Hash(), parentHash)
	}
	return "/"
}
func PermaLink(c models.Content, handle string) string {
	return fmt.Sprintf("/~%s/%s", handle, c.Hash())
}
func LoadItem(c models.Content) Item {
	i := Item{
		Hash:        c.Hash(),
		UpdatedAt:   c.UpdatedAt,
		SubmittedAt: c.SubmittedAt,
		SubmittedBy: c.SubmittedByAccount.Handle,
		MimeType:    c.MimeType,
		Score:       c.Score,
		flags:       c.Flags,
		metadata:    c.Metadata,
		path:        c.Path,
		fullPath:    c.FullPath(),
		id:          c.Id,
		isTop:       c.IsTop(),
		isLink:      c.IsLink(),
		parentLink:  ParentLink(c),
		permaLink:   PermaLink(c, c.SubmittedByAccount.Handle),
		opLink:      OPLink(c),
	}
	if len(c.Title) > 0 {
		i.Title = string(c.Title)
	}
	if len(c.Data) > 0 {
		i.Data = string(c.Data)
	}
	return i
}

// HandleIndex serves / request
func HandleIndex(w http.ResponseWriter, r *http.Request) {
	m := indexModel{Title: "Index", InvertedTheme: IsInverted(r)}
	log.WithFields(log.Fields{
		"index": "index page",
	})

	ShowItemData = false
	var err error
	var items []models.Content
	items, err = models.LoadOPItems(Db, MaxContentItems)
	if err != nil {
		log.Error(err)
		HandleError(w, r, http.StatusNotFound, err)
		return
	}
	m.Items = loadComments(items)

	_, err = LoadVotes(CurrentAccount, m.Items.getItems())
	if err != nil {
		log.Error(err)
		HandleError(w, r, http.StatusNotFound, err)
		return
	}

	RenderTemplate(r, w, "listing", m)
}
