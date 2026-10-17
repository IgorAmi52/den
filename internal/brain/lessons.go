package brain

import (
	"fmt"
	"strings"

	"github.com/IgorAmi52/den/internal/helper"
)

// MaximumLessonsLength is how long den's lessons file may grow, in characters, before den compacts it.
const MaximumLessonsLength = 1500

// compactedLessonsLength is what a compaction aims under, so the file has room to grow again.
const compactedLessonsLength = 900

const lessonSystem = "You are den. You watch a developer's AI coding agent sessions and offer cards: a folder, context lines, a link, " +
	"a rule for every repo. The user just said no to one of your cards. Learn from it: answer lesson, one short sentence of at most 20 words, " +
	"an instruction to yourself for judging future sessions in any repo, like \"A long session that touched many parts of the repo is not a folder.\" " +
	"Base it on what the user said about the card when they said something, otherwise on what is plainly wrong with the card. " +
	"Make it general, never about this card's names or files. Answer \"\" when the lessons already say it, or when a plain no " +
	"shows nothing general, which is common: the user may just not want it now."

const compactLessonsSystem = "You are den. This is your file of lessons learned from the cards the user said no to; it grew too long. " +
	"Rewrite it shorter: merge lessons that say the same thing, keep every distinct one, drop none that still matters. " +
	"lessons: the whole new file, a markdown list of short imperative lines, under %d characters, nothing else."

type LessonAnswer struct {
	Lesson string `json:"lesson"`
}

type CompactLessonsAnswer struct {
	Lessons string `json:"lessons"`
}

// LessonRequest asks what a rejected card teaches. card is the card spelled out, with anything said about it.
func LessonRequest(card string, lessons string) helper.Request {
	return helper.Request{
		System: lessonSystem,
		Prompt: "THE CARD THE USER SAID NO TO\n" + card + "\n\nLESSONS SO FAR\n" + orNone(lessons) + "\n",
		Schema: objectSchema(map[string]any{"lesson": stringSchema()}),
		Tier:   helper.Reasoning,
	}
}

func CompactLessonsRequest(lessons string) helper.Request {
	return helper.Request{
		System: fmt.Sprintf(compactLessonsSystem, compactedLessonsLength),
		Prompt: "LESSONS\n" + lessons + "\n",
		Schema: objectSchema(map[string]any{"lessons": stringSchema()}),
		Tier:   helper.Reasoning,
	}
}

// AddLesson appends a lesson as a list line, or returns the lessons unchanged when there's none.
func AddLesson(lessons string, lesson string) string {
	lesson = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lesson), "- "))
	if lesson == "" {
		return lessons
	}
	if lessons != "" && !strings.HasSuffix(lessons, "\n") {
		lessons += "\n"
	}
	return lessons + "- " + lesson + "\n"
}

// lessonsSection is what reviews and chats read of the lessons, after den's rules.
func lessonsSection(lessons string) string {
	if strings.TrimSpace(lessons) == "" {
		return ""
	}
	return "\nLESSONS FROM THE USER'S ANSWERS (follow them over the rules above)\n" + lessons + "\n"
}
