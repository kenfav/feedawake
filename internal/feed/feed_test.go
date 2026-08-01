package feed

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestGenerate(t *testing.T) {
    dir:=t.TempDir(); csv:=filepath.Join(dir,"ids.csv"); out:=filepath.Join(dir,"docs","index.html")
    if err:=os.WriteFile(csv,[]byte("101990000,101990040,101979080,not-an-id\n"),0o644);err!=nil{t.Fatal(err)}
    c:=Config{CSVPath:csv,OutputPath:out,Locale:"S",Count:5,ArticlesPerIssue:13,MinYear:1980,MaxYear:2000}
    if err:=Generate(c);err!=nil{t.Fatal(err)}
    b,err:=os.ReadFile(out);if err!=nil{t.Fatal(err)}; s:=string(b)
    if got:=strings.Count(s,"class=\"card\"");got!=5{t.Fatalf("cards=%d, want 5",got)}
    if strings.Contains(s,"docid=101990000")||strings.Contains(s,"docid=101990040"){t.Fatal("se generĂ³ enlace a un sumario")}
    if !strings.Contains(s,"wtlocale=S"){t.Fatal("locale ausente")}
}

func TestInvalidLocale(t *testing.T){
    if err:=validate(Config{Locale:"X",Count:5,ArticlesPerIssue:13});err==nil{t.Fatal("se esperaba error")}
}

func TestBuildWOLURL(t *testing.T) {
	got, err := buildWOLURL("T", "101981484")
	if err != nil {
		t.Fatal(err)
	}

	want := "https://wol.jw.org/pt/wol/d/r5/lp-t/101981484"

	if got != want {
		t.Fatalf("buildWOLURL() = %q, want %q", got, want)
	}
}

func TestTruncateTextPreservesUnicode(t *testing.T) {
	got := truncateText("Informação útil para você", 12)

	if got != "Informação …" {
		t.Fatalf("truncateText() = %q", got)
	}
}

func TestExtractArticleMetadata(t *testing.T) {
	html := `
		<html>
			<body>
				<main>
					<h1 id="p1" data-pid="1">
						Un título interesante
					</h1>
					<p id="p2" data-pid="2">
						Este es el primer párrafo del artículo.
					</p>
				</main>
			</body>
		</html>
	`

	document, err := goquery.NewDocumentFromReader(
		strings.NewReader(html),
	)
	if err != nil {
		t.Fatal(err)
	}

	title := firstText(
		document,
		"#p1",
		`[data-pid="1"]`,
	)

	excerpt := firstText(
		document,
		"#p2",
		`[data-pid="2"]`,
	)

	if title != "Un título interesante" {
		t.Fatalf("title = %q", title)
	}

	if excerpt != "Este es el primer párrafo del artículo." {
		t.Fatalf("excerpt = %q", excerpt)
	}
}




