package main

import (
	"errors"

	"github.com/unixsuperhero/ochazuke"
)

// The prompt (from o-csv): one single-line input of the field noun
// (insert pinned) in one modal, for the verbs that need a name in the TUI
// (worktree add, branch create). enter runs the verb with the text, esc
// cancels. The request rides in App.ask; the modal's payload holds the
// label.

const promptFrame = "o-git-prompt"
const fieldID = "field"

// question is the verb an open prompt runs: the request, with the text
// put in its arg.
type question struct {
	request ochazuke.Request
	arg     string
}

func (a *App) registerPrompt() {
	r := a.r
	r.Define("field").Pin("insert").
		Verb("commit", func(r *ochazuke.Runtime, c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			q := a.ask
			if q == nil || r.Focus != fieldID {
				return nil, errors.New("no prompt is open")
			}
			text := string(a.field.Text)
			a.ask = nil
			if err := r.Pop(); err != nil {
				return nil, err
			}
			if text == "" {
				return nil, nil
			}
			req := q.request
			req.Args[q.arg] = text
			return r.Execute(req)
		}, "Close the prompt and run its verb with the field's text (empty does nothing)").
		Verb("cancel", func(r *ochazuke.Runtime, c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			if a.ask == nil {
				return nil, errors.New("no prompt is open")
			}
			a.ask = nil
			return nil, r.Pop()
		}, "Close the prompt without running its verb").
		Mode("insert").
		Bind("enter", ochazuke.Do("field.commit")).
		Bind("esc", ochazuke.Do("field.cancel"))
	a.field = ochazuke.NewInput(fieldID, "", false)
	a.m.ModalViews[promptFrame] = ochazuke.ModalView{Title: "Prompt", Render: func(r *ochazuke.Runtime, body *ochazuke.Instance, w, h int) ochazuke.Block {
		field := ochazuke.InputBlock(a.field, max(1, w), 1)
		return field.Offset(0, 2).WithText(body.Payload.Prompt + "\n\n" + field.Text)
	}}
}

// prompt asks for arg of noun.verb; the verb then runs in the repository
// the asking row belongs to.
func (a *App) prompt(c ochazuke.Ctx, label, noun, verb, arg string) ochazuke.Result {
	if a.ask != nil {
		return ochazuke.Failed(errors.New("a prompt is already open"))
	}
	if err := a.field.Replace(0, len(a.field.Text), ""); err != nil {
		return ochazuke.Failed(err)
	}
	a.field.Cursor = 0
	view := a.m.ModalViews[promptFrame]
	view.Title = label
	a.m.ModalViews[promptFrame] = view
	a.ask = &question{request: ochazuke.Request{Noun: noun, Verb: verb, Args: ochazuke.Arguments{"repo": a.repoWord(c)}}, arg: arg}
	if _, err := a.r.OpenModal(promptFrame, ochazuke.Payload{Prompt: label + " (in " + a.currentName + ")"}, ochazuke.WithInput(a.field, ochazuke.Kind("field"))); err != nil {
		a.ask = nil
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(nil)
}
