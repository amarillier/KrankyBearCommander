package rename

import "testing"

func TestPreviewDefaultPattern(t *testing.T) {
	got, err := Preview(Options{}, "photo.jpg", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "photo.jpg" {
		t.Fatalf("got %q, want %q", got, "photo.jpg")
	}
}

func TestPreviewPlainPatternKeepsExtension(t *testing.T) {
	got, err := Preview(Options{Pattern: "vacation"}, "photo.jpg", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "vacation.jpg" {
		t.Fatalf("got %q, want %q", got, "vacation.jpg")
	}
}

func TestPreviewNPlaceholder(t *testing.T) {
	got, err := Preview(Options{Pattern: "[N]-backup"}, "report.docx", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "report-backup.docx" {
		t.Fatalf("got %q, want %q", got, "report-backup.docx")
	}
}

func TestPreviewNRangePlaceholder(t *testing.T) {
	got, err := Preview(Options{Pattern: "[N1-3]"}, "abcdef.txt", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc.txt" {
		t.Fatalf("got %q, want %q", got, "abc.txt")
	}
}

func TestPreviewNRangeClampsOutOfBounds(t *testing.T) {
	got, err := Preview(Options{Pattern: "[N1-99]"}, "abc.txt", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc.txt" {
		t.Fatalf("got %q, want %q", got, "abc.txt")
	}
}

func TestPreviewEPlaceholder(t *testing.T) {
	got, err := Preview(Options{Pattern: "[N].[E].bak"}, "photo.jpg", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "photo.jpg.bak" {
		t.Fatalf("got %q, want %q", got, "photo.jpg.bak")
	}
}

func TestPreviewCounterDefault(t *testing.T) {
	for i, want := range []string{"img-1.jpg", "img-2.jpg", "img-3.jpg"} {
		got, err := Preview(Options{Pattern: "img-[C]"}, "x.jpg", i+1)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("index %d: got %q, want %q", i+1, got, want)
		}
	}
}

func TestPreviewCounterStartStepWidth(t *testing.T) {
	for i, want := range []string{"img-010.jpg", "img-015.jpg", "img-020.jpg"} {
		got, err := Preview(Options{Pattern: "img-[C:10,5,3]"}, "x.jpg", i+1)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("index %d: got %q, want %q", i+1, got, want)
		}
	}
}

func TestPreviewCaseUpper(t *testing.T) {
	got, err := Preview(Options{Pattern: "[N]", Case: CaseUpper}, "photo.jpg", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "PHOTO.jpg" {
		t.Fatalf("got %q, want %q", got, "PHOTO.jpg")
	}
}

func TestPreviewCaseTitle(t *testing.T) {
	got, err := Preview(Options{Pattern: "[N]", Case: CaseTitle}, "my_vacation-photo.jpg", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "My_Vacation-Photo.jpg" {
		t.Fatalf("got %q, want %q", got, "My_Vacation-Photo.jpg")
	}
}

func TestPreviewCaseSentence(t *testing.T) {
	got, err := Preview(Options{Pattern: "[N]", Case: CaseSentence}, "MY REPORT.docx", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "My report.docx" {
		t.Fatalf("got %q, want %q", got, "My report.docx")
	}
}

func TestPreviewFindReplacePlain(t *testing.T) {
	got, err := Preview(Options{Pattern: "[N]", Find: "IMG", Replace: "Photo"}, "IMG_001.jpg", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Photo_001.jpg" {
		t.Fatalf("got %q, want %q", got, "Photo_001.jpg")
	}
}

func TestPreviewFindReplaceRegex(t *testing.T) {
	got, err := Preview(Options{Pattern: "[N]", Find: `\d+`, Replace: "#", UseRegex: true}, "track42.mp3", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "track#.mp3" {
		t.Fatalf("got %q, want %q", got, "track#.mp3")
	}
}

func TestPreviewInvalidRegexErrors(t *testing.T) {
	_, err := Preview(Options{Pattern: "[N]", Find: "(", UseRegex: true}, "a.txt", 1)
	if err == nil {
		t.Fatal("expected an error for an invalid regex")
	}
}

func TestPreviewEmptyResultErrors(t *testing.T) {
	_, err := Preview(Options{Pattern: "[N]", Find: "a", Replace: "", UseRegex: false}, "a", 1)
	if err == nil {
		t.Fatal("expected an error for a pattern that produces an empty name")
	}
}

func TestPreviewNoExtension(t *testing.T) {
	got, err := Preview(Options{Pattern: "[N]-v2"}, "README", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "README-v2" {
		t.Fatalf("got %q, want %q", got, "README-v2")
	}
}

func TestPreviewBatch(t *testing.T) {
	names := []string{"a.jpg", "b.jpg", "c.jpg"}
	out, err := PreviewBatch(Options{Pattern: "photo-[C:1,1,2]"}, names)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"photo-01.jpg", "photo-02.jpg", "photo-03.jpg"}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("index %d: got %q, want %q", i, out[i], want[i])
		}
	}
}
