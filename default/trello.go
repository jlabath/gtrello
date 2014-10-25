package gtrello

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"appengine"
	"appengine/datastore"
	"appengine/delay"
	alog "appengine/log"
	"appengine/mail"
	"appengine/urlfetch"

	cm "github.com/jlabath/cmsgparser"
	tapi "github.com/jlabath/trello"
)

//our config type
var cfg struct {
	Key            string
	Token          string
	GithubSecret   string
	MaxCommentSize int
	Admins         []string
}

const OK_BODY = "<html><body>OK</body></html>"

//json github commit types - incomplete but sufficient as json decoder fills only fields that exist and ignores rest

type Payload struct {
	Before      string
	After       string
	Ref         string
	Commits     []Commit
	Compare     interface{}
	Created     interface{}
	Delete      interface{}
	Repository  interface{}
	Head_Commit interface{}
	Pusher      interface{}
}

type Commit struct {
	Id        string
	Message   string
	Timestamp string
	Url       string
	Added     []string
	Removed   []string
	Modified  []string
	Author    Author
	Committer Author
	Distinct  bool
}

type Author struct {
	Name     string
	Username string
	Email    string
}

//two datastore types

type TrelloComment struct {
	Result string `datastore:"noindex"`
}

//GithubPayload stores the github request in datastore for later processing
type GithubPayload struct {
	Payload     []byte `datastore:",noindex"`
	DateCreated time.Time
}

func NewGithubPayload() *GithubPayload {
	t := time.Now()
	return &GithubPayload{nil, t.UTC()}
}

func trelloView(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	//deal with GET, HEAD, OPTIONS
	if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
		fmt.Fprintf(w, OK_BODY)
		return
	}
	//read body

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		ctx.Errorf("Trouble reading request body: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()
	//read the mac header
	messageMAC, err := parseSignature(r.Header.Get("X-Hub-Signature"))
	if err != nil {
		ctx.Errorf("Unable to parse HMAC Signature %s", err)
		http.Error(w, "Signature missing or unexpected", http.StatusBadRequest)
		return
	}
	//verify it on live environment
	if checkMAC(body, messageMAC, []byte(cfg.GithubSecret)) == false && appengine.IsDevAppServer() == false {
		ctx.Errorf("Message HMAC verification failed")
		http.Error(w, "Signature failed", http.StatusUnauthorized)
		return
	}
	//process it here
	g := NewGithubPayload()
	g.Payload = body
	//save the github payload first
	gpKey, err := datastore.Put(ctx, datastore.NewIncompleteKey(ctx, "GithubPayload", nil), g)
	if err != nil {
		ctx.Errorf("Trouble saving the GithubPayload: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	trelloWorkerViewLater.Call(ctx, gpKey.Encode())
	fmt.Fprintf(w, OK_BODY)
}

//trelloWorkerView is the one doing the work of processing commit
func trelloWorkerView(ctx appengine.Context, payloadKey string) error {
	var (
		err error
	)
	if payloadKey == "" {
		ctx.Errorf("GithubPayload Key is empty quitting.")
		return nil
	}
	gpKey, err := datastore.DecodeKey(payloadKey)
	if err != nil {
		ctx.Errorf("Trouble decoding GithubPayload key: %s", err)
		return nil
	}
	g := NewGithubPayload()
	//get the github payload
	err = datastore.Get(ctx, gpKey, g)
	if err != nil {
		ctx.Errorf("Trouble getting the GithubPayload: %s", err)
		return nil
	}
	if err = processGithubPayload(gpKey, g, ctx); err != nil {
		ctx.Errorf("Trouble processing the GithubPayload %#v you can try to re-run %s %d error was: %s", g, gpKey.Kind(), gpKey.IntID(), err)
		return nil
	}
	return nil
}

var trelloWorkerViewLater = delay.Func("trelloView1", trelloWorkerView)

//processGithubPayload takes a GithubPayload object parses the json and then calls process commit for each commit
func processGithubPayload(gKey *datastore.Key, g *GithubPayload, ctx appengine.Context) (err error) {
	var (
		p Payload
	)
	//ctx.Debugf("payload was %s", payload)
	err = json.Unmarshal([]byte(g.Payload), &p)
	if err != nil {
		ctx.Errorf("payload decoding %s error was: %s", g.Payload, err)
		return
	}
	//this is likely just sanity thing
	for i := 0; i < len(p.Commits); i++ {
		url := p.Commits[i].Url
		author := p.Commits[i].Author.Name
		msg := p.Commits[i].Message
		if isValidStr(url) == false || isValidStr(author) == false || isValidStr(msg) == false {
			err = fmt.Errorf("missing proper fields url:%s, message:%s, author_name:%s", url, author, msg)
			ctx.Errorf("payload commit content error was: %s", err)
			return
		}
	}
	for i := 0; i < len(p.Commits); i++ {
		err = processCommit(ctx, p.Commits[i])
		if err != nil {
			ctx.Errorf("failed on commit %#v with %s", p.Commits[i], err.Error())
			return
		}
	}
	return
}

func processCommit(ctx appengine.Context, c Commit) error {
	root, err := cm.Parse(c.Message)
	ctx.Debugf("Message: %s", c.Message)
	if err != nil {
		ctx.Errorf("cmsgparser error: %s", err)
		return err
	}
	//build out the commit msg by simply concatenating all text nodes
	var commentBuf bytes.Buffer
	commentBuf.Grow(cfg.MaxCommentSize)
	//add author
	commentBuf.WriteString(c.Author.Name)
	commentBuf.WriteString("\n")
	for _, v := range root.Children() {
		if v.Type == cm.TextNode {
			commentBuf.WriteString(v.Value)
		}
	}
	//max buf length
	maxBufLen := cfg.MaxCommentSize - (len(c.Url) + 1) // +1 for \n
	//shrink it to fit the MaxCommentSize
	if commentBuf.Len() > maxBufLen {
		commentBuf.Truncate(maxBufLen)
	}
	//add url
	commentBuf.WriteString("\n")
	commentBuf.WriteString(c.Url)
	//here call comment card for all cards
	for _, v := range root.Children() {
		if v.Type == cm.LinkNode {
			actionCardLater.Call(ctx, v, commentBuf.String(), c.Url, v.Children()) //need to pass children extra since they don't get gobbed by delay package
		}
	}
	return nil
}

func actionCard(ctx appengine.Context, link *cm.Node, comment string, url string, children []*cm.Node) error {
	ctx.Debugf("link is %#v\nurl is %s\ncomment is %s\nchildren are %#v", link, url, comment, children)
	var ent TrelloComment
	tcKey := datastore.NewKey(ctx, "TrelloComment", link.Value+url, 0, nil)
	if geterr := datastore.Get(ctx, tcKey, &ent); geterr != nil && geterr != datastore.ErrNoSuchEntity {
		ctx.Errorf("commentCard datastore get error %s", geterr)
		return geterr
	}
	if len(ent.Result) > 0 {
		ctx.Infof("TrelloComment %s already exists aborting.", tcKey)
		return nil
	}
	client := urlfetch.Client(ctx)
	auth := tapi.Auth{Key: cfg.Key, Token: cfg.Token}
	cardId := getCardId(link.Value)
	err := tapi.CommentPost(&auth, client, cardId, comment)
	if err != nil {
		ctx.Errorf("CommentPost for %s failed. Error: %s", link.Value, err)
		return err
	}
	//save the fact that we successfully commented
	ent.Result = "OK"
	_, errign := datastore.Put(ctx, tcKey, &ent)
	if errign != nil {
		ctx.Errorf("commentCard datastore put error %s", errign)
		return nil
	}
	//move card if applicable
	if len(children) == 0 {
		//no move
		return nil
	}
	moveToNode := children[0]
	card, err := tapi.CardGet(&auth, client, cardId)
	if err != nil {
		ctx.Errorf("CardGet failed %s", err)
		return nil //returning nil as move is ok to fail
	}
	board, err := tapi.BoardGet(&auth, client, card.IdBoard())
	if err != nil {
		ctx.Errorf("BoardGet failed %s", err)
		return nil //returning nil as move is ok to fail
	}
	lists, err := tapi.BoardListsGet(&auth, client, board.Id())
	if err != nil {
		ctx.Errorf("BoardListsGet failed %s", err)
		return nil //returning nil as move is ok to fail
	}
	var destList tapi.List
	for _, l := range lists {
		if l.Name() == moveToNode.Value {
			destList = l
			break
		}
	}
	if destList != nil {
		_, err := tapi.CardListPut(&auth, client, card.Id(), destList.Id())
		if err != nil {
			ctx.Errorf("CardListPut failed %s", err)
			return nil //returning nil as move is ok to fail
		}
	}
	return nil
}

var actionCardLater = delay.Func("actionCard1", actionCard)

func logView(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	start := time.Now().Add(-2 * time.Hour) //only last two hours
	query := &alog.Query{
		AppLogs:   true,
		StartTime: start,
	}
	var textBuf bytes.Buffer

	for results := query.Run(c); ; {
		record, err := results.Next()
		if err == alog.Done {
			break
		}
		if err != nil {
			c.Errorf("Failed to retrieve next log: %v", err)
			break
		}
		if record.Status > 400 {
			textBuf.WriteString(record.Combined)
			textBuf.WriteString("\n")
		}
	}
	if textBuf.Len() > 0 {
		//send email
		sendAdminEmail(c, textBuf.String())
	}
	fmt.Fprintf(w, "<HTML><BODY><PRE>%s</PRE></BODY>", textBuf.String())
}

func sendAdminEmail(c appengine.Context, message string) {
	msg := &mail.Message{
		Sender:  "GAdvHook <gae@gadvhook.appspotmail.com>",
		To:      cfg.Admins,
		Subject: "System Notification",
		Body:    message,
	}
	if err := mail.Send(c, msg); err != nil {
		c.Errorf("Couldn't send email: %v", err)
	}
}

//init loads configuration and setups url we listen on
func init() {
	f, err := os.Open("config.json")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		if derr := dec.Decode(&cfg); derr == io.EOF {
			break
		} else if derr != nil {
			log.Fatal(derr)
			break
		}
	}
	//define url handlers
	http.HandleFunc("/logview/", logView)
	http.HandleFunc("/", trelloView)
}
