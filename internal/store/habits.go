package store

type HabitState string

const (
	// HabitNoticed is a guess den keeps quiet about until it's sure, or until you answered a question about it.
	HabitNoticed HabitState = "noticed"
	HabitActive  HabitState = "active"
	// HabitRetired is never suggested from again, and never learned again from the same evidence.
	HabitRetired HabitState = "retired"
)

// habitRetiresAfter is how many rejected suggestions turn a habit off.
const habitRetiresAfter = 2

// Habit is a way of working den noticed, with the evidence for it. It's data, not code,
// so den learns any team's way of working without it being built in.
type Habit struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	State       HabitState `json:"state"`
	// Seen counts the times the pattern shows up right now.
	Seen     int `json:"seen"`
	Accepted int `json:"accepted,omitempty"`
	Rejected int `json:"rejected,omitempty"`
}

func (repository *Repository) FindHabit(name string) *Habit {
	for index := range repository.Habits {
		if repository.Habits[index].Name == name {
			return &repository.Habits[index]
		}
	}
	return nil
}

// NoteHabit records how often a pattern shows up, keeping it a quiet guess the first time.
// It reports whether anything changed.
func (repository *Repository) NoteHabit(name string, description string, seen int) bool {
	habit := repository.FindHabit(name)
	if habit == nil {
		if seen == 0 {
			return false
		}
		repository.Habits = append(repository.Habits, Habit{Name: name, Description: description, State: HabitNoticed, Seen: seen})
		return true
	}
	if habit.Seen == seen && habit.Description == description {
		return false
	}
	habit.Seen, habit.Description = seen, description
	return true
}

func (repository *Repository) IsHabitActive(name string) bool {
	habit := repository.FindHabit(name)
	return habit != nil && habit.State == HabitActive
}

func (repository *Repository) setHabitState(name string, state HabitState) {
	if habit := repository.FindHabit(name); habit != nil {
		habit.State = state
		return
	}
	repository.Habits = append(repository.Habits, Habit{Name: name, State: state})
}

// countHabitAnswer strengthens a habit on a yes, and retires it after enough noes.
func (repository *Repository) countHabitAnswer(name string, accepted bool) {
	habit := repository.FindHabit(name)
	if habit == nil {
		return
	}
	if accepted {
		habit.Accepted++
		return
	}
	habit.Rejected++
	if habit.Rejected >= habitRetiresAfter {
		habit.State = HabitRetired
	}
}
