package brain

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/store"
)

// recentChatMessages is how many of the user's messages den reads together, so "link docs/PLAN.md"
// followed by "to Eval" is one request.
const recentChatMessages = 3

var (
	linkWords   = toSet("link linked linking attach")
	unlinkWords = toSet("unlink unlinked detach")
)

type ChatMessage struct {
	FromDen bool
	Text    string
	// Action is what den will do once the user presses enter, for a change they asked for.
	Action *ChatAction
	// Asks is set when den asked what's missing from a request to "link" or "unlink",
	// so the user's next message continues it.
	Asks string
}

// ChatAction is a change the user asked for in the chat; it waits for their enter.
type ChatAction struct {
	FolderId string
	// LinkedPath is the file to link as the folder's context, or empty to unlink the folder's file.
	LinkedPath string
}

// ChatContext is what den reads to answer without a model.
type ChatContext struct {
	Repository       *store.Repository
	Digests          map[string]agent.Digest
	WorkingDirectory string
	// FolderId is the folder the chat is about, which a request that names no folder means.
	FolderId string
}

// Reply answers the user in the chat. Without a model, den understands linking a file as a folder's context,
// and unlinking it; for anything else it says its model isn't connected. A model-backed reply will answer
// from the sessions and folder notes, and turn requests like "put it in Agent API instead" into suggestions.
func Reply(conversation []ChatMessage, about *store.Suggestion, chatContext ChatContext) ChatMessage {
	if reply, understood := linkReply(conversation, chatContext); understood {
		return reply
	}
	text := "I can't answer that yet: my model isn't connected. Once it is, I'll answer from your sessions and folder notes, " +
		"and turn what you ask for into suggestions you confirm. Right now I can link a file as a folder's context: \"link docs/PLAN.md to Eval\"."
	if about != nil {
		text += "\n\nFor this suggestion, press esc and then e to change it by hand, or n to reject it so I don't offer it again."
	}
	return ChatMessage{FromDen: true, Text: text}
}

// linkReply understands "link <file> to <folder>" and "unlink <folder>", filling in from the user's previous messages
// what the latest one leaves out. It reports whether the message was about linking at all.
func linkReply(conversation []ChatMessage, chatContext ChatContext) (ChatMessage, bool) {
	var userMessages []string
	for _, message := range conversation {
		if !message.FromDen {
			userMessages = append(userMessages, message.Text)
		}
	}
	if len(userMessages) == 0 {
		return ChatMessage{}, false
	}
	latestWords := words(userMessages[len(userMessages)-1])
	isUnlink := slices.ContainsFunc(latestWords, func(word string) bool { return unlinkWords[word] })
	isLink := !isUnlink && slices.ContainsFunc(latestWords, func(word string) bool { return linkWords[word] })
	if !isLink && !isUnlink && len(conversation) >= 2 {
		previousAnswer := conversation[len(conversation)-2]
		isLink, isUnlink = previousAnswer.Asks == "link", previousAnswer.Asks == "unlink"
	}
	if !isLink && !isUnlink {
		return ChatMessage{}, false
	}
	recent := userMessages[max(len(userMessages)-recentChatMessages, 0):]
	repository := chatContext.Repository
	var folder *store.Folder
	var filePath string
	for index := len(recent) - 1; index >= 0; index-- {
		if folder == nil {
			folder = mentionedFolder(repository, recent[index])
		}
		if filePath == "" {
			if mentioned := agent.MarkdownFilesIn(recent[index], chatContext.WorkingDirectory, repository.RepositoryPath); len(mentioned) > 0 {
				filePath = mentioned[0]
			}
		}
	}
	if folder == nil {
		folder = repository.FindFolder(chatContext.FolderId)
	}
	reply := func(text string, action *ChatAction) (ChatMessage, bool) {
		return ChatMessage{FromDen: true, Text: text, Action: action}, true
	}
	ask := func(text string, request string) (ChatMessage, bool) {
		return ChatMessage{FromDen: true, Text: text, Asks: request}, true
	}
	if isUnlink {
		if folder == nil {
			return ask("Which folder should I unlink? Name it, like: unlink Eval.", "unlink")
		}
		if folder.LinkedPath == "" {
			return reply(folder.Name+" has no linked file.", nil)
		}
		return reply(fmt.Sprintf("Unlink %s from %s? New sessions there start without it; the file itself stays. ↵ unlinks it.",
			filepath.Base(folder.LinkedPath), folder.Name), &ChatAction{FolderId: folder.Id})
	}
	if filePath == "" && folder == nil {
		return ask("Tell me the file and the folder, like: link docs/PLAN.md to Eval.", "link")
	}
	if filePath == "" {
		text := "Which file? Give me its path, like docs/PLAN.md."
		if candidates := writtenNotesFiles(chatContext, folder); len(candidates) > 0 {
			text += " Agents in " + folder.Name + " wrote " + strings.Join(candidates, ", ") + "."
		}
		return ask(text, "link")
	}
	if folder == nil {
		return ask(fmt.Sprintf("Which folder should %s be the context of? Name it, like: to Eval.", filepath.Base(filePath)), "link")
	}
	return reply(fmt.Sprintf("Link %s as %s's context? Every new session there starts with it, and den never writes it. ↵ links it.",
		relativeTo(repository.RepositoryPath, filePath), folder.Name), &ChatAction{FolderId: folder.Id, LinkedPath: filePath})
}

// mentionedFolder finds the folder whose whole name the text says, the longest name first, so "Agent API" beats "Agent".
// Paths don't count, so docs/EVAL_PLAN.md doesn't name the Eval folder.
func mentionedFolder(repository *store.Repository, text string) *store.Folder {
	var withoutPaths []string
	for _, field := range strings.Fields(text) {
		if !strings.Contains(field, "/") && !strings.Contains(field, ".md") {
			withoutPaths = append(withoutPaths, field)
		}
	}
	textWords := words(strings.Join(withoutPaths, " "))
	var found *store.Folder
	for index := range repository.Folders {
		folder := &repository.Folders[index]
		folderWords := words(folder.Name)
		if len(folderWords) > 0 && containsAll(textWords, folderWords) && (found == nil || len(folderWords) > len(words(found.Name))) {
			found = folder
		}
	}
	return found
}

// writtenNotesFiles lists the markdown files of real size agents wrote in a folder's sessions, at most three.
func writtenNotesFiles(chatContext ChatContext, folder *store.Folder) []string {
	var found []string
	for _, session := range chatContext.Repository.Sessions {
		if session.FolderId != folder.Id {
			continue
		}
		digest := chatContext.Digests[session.SessionId]
		for _, filePath := range digest.TouchedFiles {
			if !filepath.IsAbs(filePath) {
				filePath = filepath.Join(digest.WorkingDirectory, filePath)
			}
			name := relativeTo(chatContext.Repository.RepositoryPath, filePath)
			if len(found) < 3 && !slices.Contains(found, name) && isNotesFile(chatContext.Repository, filePath) {
				found = append(found, name)
			}
		}
	}
	return found
}

func relativeTo(directory string, filePath string) string {
	if relativePath, err := filepath.Rel(directory, filePath); err == nil && !strings.HasPrefix(relativePath, "..") {
		return relativePath
	}
	return filePath
}
