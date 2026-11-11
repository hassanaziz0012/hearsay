package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Dictation stats: every finished dictation adds one line of numbers to stats.jsonl in
// the config dir, and the window's Stats page is worked out from those lines. The text
// itself is never written anywhere.

// statRecord is one line of stats.jsonl.
type statRecord struct {
	T  int64 `json:"t"`            // when it ended, Unix seconds
	W  int   `json:"w"`            // words
	C  int   `json:"c"`            // characters
	Ms int64 `json:"ms,omitempty"` // how long the phone listened; 0 when not known
	// "ok" (pasted or copied), "cancelled", or "empty" (nothing heard).
	R string `json:"r"`
}

const (
	// A phone left listening for hours wasn't someone talking: such a time isn't counted.
	maxSpeakMs = 2 * 60 * 60 * 1000
	// The typing speed "time saved" is compared with until the typing test has been taken.
	defaultTypingWPM = 40
)

func statsPath() string { return filepath.Join(configDir(), "stats.jsonl") }

func (c config) statsOn() bool { return c.Stats == nil || *c.Stats }

// markListening notes when the phone started listening, for whichever process stops it
// (a hotkey press is a process of its own).
func markListening() {
	writeAtomic(runFile("since"), []byte(strconv.FormatInt(time.Now().UnixMilli(), 10)))
}

// listeningSince returns that time (zero if there is none) and forgets it.
func listeningSince() time.Time {
	p := runFile("since")
	b, err := os.ReadFile(p)
	os.Remove(p)
	ms, perr := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || perr != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// recordDictation adds the dictation that just ended to the stats. stopped is when the
// user ended it (before the phone's wait for the last words).
func (a *app) recordDictation(text string, paste bool, stopped time.Time) {
	since := listeningSince()
	if a.noStats || !a.cfg.statsOn() {
		return
	}
	rec := statRecord{T: time.Now().Unix(), R: "ok"}
	switch {
	case !paste:
		rec.R = "cancelled"
	case strings.TrimSpace(text) == "":
		rec.R = "empty"
	default:
		rec.W = len(strings.Fields(text))
		rec.C = len([]rune(text))
	}
	if since.IsZero() {
		// A stop or cancel while nothing was listening isn't a dictation.
		if rec.R != "ok" {
			return
		}
	} else if ms := stopped.Sub(since).Milliseconds(); ms > 0 && ms <= maxSpeakMs {
		rec.Ms = ms
	}
	if err := appendStat(rec); err != nil {
		a.logf("stats: %v", err)
	}
}

// appendStat adds one line with a single write, so the tray and a hotkey press writing
// at the same moment can't mix their lines.
func appendStat(rec statRecord) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configDir(), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(statsPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// loadStats reads every record. Lines that don't parse (a hand edit, a write cut short)
// are skipped.
func loadStats() ([]statRecord, error) {
	b, err := os.ReadFile(statsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var recs []statRecord
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		var r statRecord
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.T > 0 {
			recs = append(recs, r)
		}
	}
	return recs, nil
}

// statsCSV is the records as a spreadsheet would want them, with times in loc.
func statsCSV(recs []statRecord, loc *time.Location) string {
	var b strings.Builder
	b.WriteString("time,words,characters,seconds,outcome\n")
	for _, r := range recs {
		secs := ""
		if r.Ms > 0 {
			secs = strconv.FormatFloat(float64(r.Ms)/1000, 'f', 1, 64)
		}
		// The outcome is the only field that could hold anything (a hand-edited file).
		outcome := strings.Map(func(c rune) rune {
			if c == ',' || c == '"' || c < ' ' {
				return -1
			}
			return c
		}, r.R)
		fmt.Fprintf(&b, "%s,%d,%d,%s,%s\n", time.Unix(r.T, 0).In(loc).Format("2006-01-02 15:04:05"), r.W, r.C, secs, outcome)
	}
	return b.String()
}

// Stats is what the window's Stats page shows.
type Stats struct {
	Today Span `json:"today"`
	Week  Span `json:"week"`  // since Monday
	Month Span `json:"month"` // this calendar month
	Total Span `json:"total"`

	// Every day from the Monday 53 weeks before this week's up to today, oldest first.
	Days []DayStat `json:"days"`
	// Words by the hour and by the weekday (Monday first) they were dictated in, all time.
	ByHour    [24]int `json:"byHour"`
	ByWeekday [7]int  `json:"byWeekday"`

	// Days in a row with a dictation. A streak is still alive if today has none yet.
	Streak        int `json:"streak"`
	LongestStreak int `json:"longestStreak"`

	BestDay      *DayStat `json:"bestDay"`      // nil before the first dictation
	LongestWords int      `json:"longestWords"` // the longest single dictation
	LongestDate  string   `json:"longestDate"`

	// Speaking time and pace come from the dictations whose length is known.
	SpeakMs        int64 `json:"speakMs"`
	PaceWPM        int   `json:"paceWpm"`
	TypingWPM      int   `json:"typingWpm"`
	TypingMeasured bool  `json:"typingMeasured"` // false: the default, no test taken
	// What typing the same words at TypingWPM would have taken, less SpeakMs.
	SavedMs int64 `json:"savedMs"`

	Cancelled int    `json:"cancelled"`
	Empty     int    `json:"empty"`
	Since     string `json:"since"` // day of the first record, "" if none
}

// Span is the dictations that produced text within some period.
type Span struct {
	Words      int `json:"words"`
	Dictations int `json:"dictations"`
}

func (s *Span) add(r statRecord) {
	s.Words += r.W
	s.Dictations++
}

type DayStat struct {
	Date       string `json:"date"` // 2006-01-02, local time
	Words      int    `json:"words"`
	Dictations int    `json:"dictations"`
}

const dayFormat = "2006-01-02"

// summarize works the stats out of the records. Days are days in now's time zone.
// typingWPM is the measured typing speed, 0 if the test was never taken.
func summarize(recs []statRecord, now time.Time, typingWPM int) Stats {
	s := Stats{TypingWPM: typingWPM, TypingMeasured: typingWPM > 0, Days: []DayStat{}}
	if !s.TypingMeasured {
		s.TypingWPM = defaultTypingWPM
	}
	loc := now.Location()
	y, m, d := now.Date()
	// Days are counted with time.Date, which stays right across clock changes.
	day := func(offset int) time.Time { return time.Date(y, m, d+offset, 0, 0, 0, 0, loc) }
	sinceMonday := (int(now.Weekday()) + 6) % 7
	today, monday := day(0).Format(dayFormat), day(-sinceMonday).Format(dayFormat)
	month := now.Format("2006-01")

	days := map[string]*DayStat{}
	var pacedWords int
	var typingMs float64
	for _, r := range recs {
		t := time.Unix(r.T, 0).In(loc)
		date := t.Format(dayFormat)
		if s.Since == "" || date < s.Since {
			s.Since = date
		}
		switch r.R {
		case "cancelled":
			s.Cancelled++
			continue
		case "empty":
			s.Empty++
			continue
		}
		ds := days[date]
		if ds == nil {
			ds = &DayStat{Date: date}
			days[date] = ds
		}
		ds.Words += r.W
		ds.Dictations++
		s.Total.add(r)
		if date == today {
			s.Today.add(r)
		}
		if date >= monday {
			s.Week.add(r)
		}
		if strings.HasPrefix(date, month) {
			s.Month.add(r)
		}
		s.ByHour[t.Hour()] += r.W
		s.ByWeekday[(int(t.Weekday())+6)%7] += r.W
		if r.W > s.LongestWords {
			s.LongestWords, s.LongestDate = r.W, date
		}
		if r.Ms > 0 {
			s.SpeakMs += r.Ms
			pacedWords += r.W
			typingMs += float64(r.W) * 60000 / float64(s.TypingWPM)
		}
	}
	if s.SpeakMs > 0 {
		s.PaceWPM = int(float64(pacedWords)*60000/float64(s.SpeakMs) + 0.5)
		s.SavedMs = max(0, int64(typingMs)-s.SpeakMs)
	}

	for i := -(53*7 + sinceMonday); i <= 0; i++ {
		date := day(i).Format(dayFormat)
		if ds := days[date]; ds != nil {
			s.Days = append(s.Days, *ds)
		} else {
			s.Days = append(s.Days, DayStat{Date: date})
		}
	}

	// Streaks, over every day there is, numbered so that consecutive days differ by one.
	var nums []int
	for date, ds := range days {
		if s.BestDay == nil || ds.Words > s.BestDay.Words || (ds.Words == s.BestDay.Words && date < s.BestDay.Date) {
			best := *ds
			s.BestDay = &best
		}
		nums = append(nums, dayNumber(date))
	}
	sort.Ints(nums)
	run, todayNum := 0, dayNumber(today)
	for i, n := range nums {
		if i > 0 && n == nums[i-1]+1 {
			run++
		} else {
			run = 1
		}
		s.LongestStreak = max(s.LongestStreak, run)
		if i == len(nums)-1 && n >= todayNum-1 {
			s.Streak = run
		}
	}
	return s
}

// dayNumber numbers a 2006-01-02 date by days since 1970.
func dayNumber(date string) int {
	t, err := time.Parse(dayFormat, date)
	if err != nil {
		return 0
	}
	return int(t.Unix() / 86400)
}
