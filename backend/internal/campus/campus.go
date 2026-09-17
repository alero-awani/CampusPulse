// Package campus describes NorthBridge University's buildings, rooms, and demo
// accounts. The setup command and the simulator share it, so they agree on room IDs.
package campus

import (
	"fmt"
	"strings"

	"campuspulse/internal/model"
)

type Room struct {
	ID       string
	Name     string
	Building string
	Floor    int
	Type     model.RoomType
	Capacity int
}

type Building struct {
	Name string
	// BaseLoadKW is the building's daytime electrical load with nobody inside
	// (lighting, HVAC, servers). The simulator uses it for energy readings.
	BaseLoadKW float64
	Rooms      []Room
}

type roomSpec struct {
	name     string
	floor    int
	kind     model.RoomType
	capacity int
}

var defaultBuildings = []struct {
	name   string
	baseKW float64
	rooms  []roomSpec
}{
	{"Library-A", 18, []roomSpec{
		{"A101", 1, model.RoomLibrary, 120},
		{"A102", 1, model.RoomStudySpace, 12},
		{"A201", 2, model.RoomLibrary, 80},
		{"A202", 2, model.RoomStudySpace, 8},
		{"A203", 2, model.RoomStudySpace, 40},
		{"A301", 3, model.RoomStudySpace, 20},
	}},
	{"Engineering-B", 55, []roomSpec{
		{"B101", 1, model.RoomLectureHall, 200},
		{"B102", 1, model.RoomLab, 30},
		{"B103", 1, model.RoomLab, 24},
		{"B201", 2, model.RoomClassroom, 40},
		{"B202", 2, model.RoomLab, 20},
		{"B203", 2, model.RoomClassroom, 35},
	}},
	{"Science-C", 35, []roomSpec{
		{"C101", 1, model.RoomLectureHall, 150},
		{"C102", 1, model.RoomLab, 28},
		{"C201", 2, model.RoomLab, 24},
		{"C202", 2, model.RoomClassroom, 40},
		{"C203", 2, model.RoomClassroom, 30},
		{"C301", 3, model.RoomStudySpace, 16},
	}},
	{"StudentHub-D", 14, []roomSpec{
		{"D101", 1, model.RoomStudySpace, 60},
		{"D102", 1, model.RoomClassroom, 25},
		{"D103", 1, model.RoomStudySpace, 10},
		{"D201", 2, model.RoomClassroom, 45},
		{"D202", 2, model.RoomStudySpace, 30},
		{"D203", 2, model.RoomLectureHall, 120},
	}},
}

// RoomID builds a room's ID from its building and name, e.g. "library-a-a203".
func RoomID(building, room string) string {
	return strings.ToLower(building + "-" + room)
}

// Build returns a campus with n buildings. The first four are always the
// NorthBridge buildings; extra "Annex" buildings are generated for load tests.
func Build(n int) []Building {
	n = max(n, 1)
	var out []Building
	for i := range n {
		var b Building
		if i < len(defaultBuildings) {
			d := defaultBuildings[i]
			b = Building{Name: d.name, BaseLoadKW: d.baseKW}
			for _, r := range d.rooms {
				b.Rooms = append(b.Rooms, Room{ID: RoomID(d.name, r.name), Name: r.name, Building: d.name, Floor: r.floor, Type: r.kind, Capacity: r.capacity})
			}
		} else {
			b = Building{Name: fmt.Sprintf("Annex-%02d", i+1), BaseLoadKW: 20}
			for j := range 6 {
				name := fmt.Sprintf("X%d%02d", j/3+1, j+1)
				b.Rooms = append(b.Rooms, Room{
					ID: RoomID(b.Name, name), Name: name, Building: b.Name, Floor: j/3 + 1,
					Type: model.RoomTypes[j%len(model.RoomTypes)], Capacity: 20 + 10*j,
				})
			}
		}
		out = append(out, b)
	}
	return out
}

type DemoUser struct {
	ID    string
	Email string
	Name  string
	Role  model.Role
}

// DemoUsers match the mock accounts in the frontend prompt, plus a second
// student to show that students only see their own service requests.
var DemoUsers = []DemoUser{
	{ID: "u-sam-rivera", Email: "student@northbridge.edu", Name: "Sam Rivera", Role: model.RoleStudent},
	{ID: "u-taylor-kim", Email: "student2@northbridge.edu", Name: "Taylor Kim", Role: model.RoleStudent},
	{ID: "u-jordan-lee", Email: "staff@northbridge.edu", Name: "Jordan Lee", Role: model.RoleStaff},
	{ID: "u-alex-morgan", Email: "admin@northbridge.edu", Name: "Alex Morgan", Role: model.RoleAdmin},
}
