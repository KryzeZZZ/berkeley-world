package dice

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"time"
)

var diceExprPattern = regexp.MustCompile(`^(\d+)d(\d+)$`)

type RollMode int

const (
	RollNormal RollMode = iota
	RollAdvantage
	RollDisadvantage
)

type Roller interface {
	RollDice(expr string) (int, error)
	RollDiceWithMode(expr string, mode RollMode) (int, error)
}

type DefaultRoller struct {
	rng *rand.Rand
}

func NewDefaultRoller() *DefaultRoller {
	return &DefaultRoller{rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

func (r *DefaultRoller) RollDice(expr string) (int, error) {
	return r.roll(expr)
}

func (r *DefaultRoller) RollDiceWithMode(expr string, mode RollMode) (int, error) {
	if mode == RollNormal {
		return r.RollDice(expr)
	}
	first, err := r.RollDice(expr)
	if err != nil {
		return 0, err
	}
	second, err := r.RollDice(expr)
	if err != nil {
		return 0, err
	}
	if mode == RollAdvantage {
		if second > first {
			return second, nil
		}
		return first, nil
	}
	if second < first {
		return second, nil
	}
	return first, nil
}

func (r *DefaultRoller) roll(expr string) (int, error) {
	matches := diceExprPattern.FindStringSubmatch(expr)
	if len(matches) != 3 {
		return 0, fmt.Errorf("unsupported dice expression: %s", expr)
	}
	count, _ := strconv.Atoi(matches[1])
	sides, _ := strconv.Atoi(matches[2])
	if count <= 0 || sides <= 0 {
		return 0, fmt.Errorf("invalid dice expression: %s", expr)
	}
	total := 0
	for i := 0; i < count; i++ {
		total += r.rng.Intn(sides) + 1
	}
	return total, nil
}
