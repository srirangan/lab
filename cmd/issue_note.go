package cmd

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"text/template"

	"github.com/rsteube/carapace"
	"github.com/spf13/cobra"
	gitlab "github.com/xanzy/go-gitlab"
	"github.com/zaquestion/lab/internal/action"
	"github.com/zaquestion/lab/internal/git"
	lab "github.com/zaquestion/lab/internal/gitlab"
)

var issueNoteCmd = &cobra.Command{
	Use:              "note [remote] <id>[:<comment_id>]",
	Aliases:          []string{"comment", "reply"},
	Short:            "Add a note or comment to an issue on GitLab",
	Long:             ``,
	Args:             cobra.MinimumNArgs(1),
	PersistentPreRun: LabPersistentPreRun,
	Run:              NoteRunFn,
}

func NoteRunFn(cmd *cobra.Command, args []string) {

	isMR := false
	if os.Args[1] == "mr" {
		isMR = true
	}

	rn, idString, err := parseArgsRemoteString(args)
	if err != nil {
		log.Fatal(err)
	}

	var (
		idNum int = 0
		reply int = 0
	)

	if strings.Contains(idString, ":") {
		ids := strings.Split(idString, ":")
		idNum, _ = strconv.Atoi(ids[0])
		reply, _ = strconv.Atoi(ids[1])
	} else {
		idNum, _ = strconv.Atoi(idString)
	}

	msgs, err := cmd.Flags().GetStringArray("message")
	if err != nil {
		log.Fatal(err)
	}

	filename, err := cmd.Flags().GetString("file")
	if err != nil {
		log.Fatal(err)
	}

	linebreak, err := cmd.Flags().GetBool("force-linebreak")
	if err != nil {
		log.Fatal(err)
	}

	if reply != 0 {
		quote, err := cmd.Flags().GetBool("quote")
		if err != nil {
			log.Fatal(err)
		}
		ReplyNote(rn, isMR, int(idNum), reply, quote, filename, linebreak)
		return
	}

	CreateNote(rn, isMR, int(idNum), msgs, filename, linebreak)
}

func CreateNote(rn string, isMR bool, idNum int, msgs []string, filename string, linebreak bool) {

	var err error

	body := ""
	if filename != "" {
		content, err := ioutil.ReadFile(filename)
		if err != nil {
			log.Fatal(err)
		}
		body = string(content)
	} else {
		body, err = noteMsg(msgs, isMR, "\n")
		if err != nil {
			_, f, l, _ := runtime.Caller(0)
			log.Fatal(f+":"+strconv.Itoa(l)+" ", err)
		}
	}

	if body == "" {
		log.Fatal("aborting note due to empty note msg")
	}

	if linebreak {
		body = textToMarkdown(body)
	}

	var (
		noteURL string
	)

	if isMR {
		noteURL, err = lab.MRCreateNote(rn, idNum, &gitlab.CreateMergeRequestNoteOptions{
			Body: &body,
		})
	} else {
		noteURL, err = lab.IssueCreateNote(rn, idNum, &gitlab.CreateIssueNoteOptions{
			Body: &body,
		})
	}
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(noteURL)
}

func noteMsg(msgs []string, isMR bool, body string) (string, error) {
	if len(msgs) > 0 {
		return strings.Join(msgs[0:], "\n\n"), nil
	}

	text, err := noteText(body)
	if err != nil {
		return "", err
	}

	if isMR {
		return git.EditFile("MR_NOTE", text)
	}
	return git.EditFile("ISSUE_NOTE", text)
}

func noteText(body string) (string, error) {
	const tmpl = `{{.InitMsg}}
{{.CommentChar}} Write a message for this note. Commented lines are discarded.`

	initMsg := body
	commentChar := git.CommentChar()

	t, err := template.New("tmpl").Parse(tmpl)
	if err != nil {
		return "", err
	}

	msg := &struct {
		InitMsg     string
		CommentChar string
	}{
		InitMsg:     initMsg,
		CommentChar: commentChar,
	}

	var b bytes.Buffer
	err = t.Execute(&b, msg)
	if err != nil {
		return "", err
	}

	return b.String(), nil
}

func ReplyNote(rn string, isMR bool, idNum int, reply int, quote bool, filename string, linebreak bool) {

	var (
		discussions []*gitlab.Discussion
		err         error
		NoteURL     string
	)

	if isMR {
		discussions, err = lab.MRListDiscussions(rn, idNum)
	} else {
		discussions, err = lab.IssueListDiscussions(rn, idNum)
	}
	if err != nil {
		log.Fatal(err)
	}
	for _, discussion := range discussions {
		for _, note := range discussion.Notes {

			if note.System {
				continue
			}

			if note.ID != reply {
				continue
			}

			body := ""
			if filename != "" {
				content, err := ioutil.ReadFile(filename)
				if err != nil {
					log.Fatal(err)
				}
				body = string(content)
			} else {
				noteBody := ""
				if quote {
					noteBody = note.Body
					noteBody = strings.Replace(noteBody, "\n", "\n>", -1)
					noteBody = ">" + noteBody + "\n"
				}
				body, err = noteMsg([]string{}, isMR, noteBody)
				if err != nil {
					_, f, l, _ := runtime.Caller(0)
					log.Fatal(f+":"+strconv.Itoa(l)+" ", err)
				}
			}

			if body == "" {
				log.Fatal("aborting note due to empty note msg")
			}

			if linebreak {
				body = textToMarkdown(body)
			}

			if isMR {
				opts := &gitlab.AddMergeRequestDiscussionNoteOptions{
					Body: &body,
				}
				NoteURL, err = lab.AddMRDiscussionNote(rn, idNum, discussion.ID, opts)
			} else {
				opts := &gitlab.AddIssueDiscussionNoteOptions{
					Body: &body,
				}
				NoteURL, err = lab.AddIssueDiscussionNote(rn, idNum, discussion.ID, opts)
			}
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(NoteURL)
		}
	}
}

func init() {
	issueNoteCmd.Flags().StringArrayP("message", "m", []string{}, "Use the given <msg>; multiple -m are concatenated as separate paragraphs")
	issueNoteCmd.Flags().StringP("file", "F", "", "Use the given file as the message")
	issueNoteCmd.Flags().Bool("force-linebreak", false, "append 2 spaces to the end of each line to force markdown linebreaks")
	issueNoteCmd.Flags().Bool("quote", false, "Quote note in reply (used with --reply only)")

	issueCmd.AddCommand(issueNoteCmd)
	carapace.Gen(issueNoteCmd).PositionalCompletion(
		action.Remotes(),
		action.Issues(issueList),
	)
}
