package feed

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"html/template"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var allowedLocales = map[string]string{"S": "Español", "T": "Português", "E": "English"}

var httpClient = &http.Client{
	Timeout: 12 * time.Second,
}

type wolLocale struct {
	Language string
	Region   string
	Library  string
}

var wolLocales = map[string]wolLocale{
	"T": {
		Language: "pt",
		Region:   "r5",
		Library:  "lp-t",
	},
	"S": {
		Language: "es",
		Region:   "r4",
		Library:  "lp-s",
	},
	"E": {
		Language: "en",
		Region:   "r1",
		Library:  "lp-e",
	},
}

type Config struct {
	CSVPath, OutputPath, Locale, SiteURL      string
	Count, ArticlesPerIssue, MinYear, MaxYear int
}

type Article struct {
	ID      string
	URL     string
	WOLURL  string
	Title   string
	Excerpt string
	Year    int
}

type PageData struct {
	Articles                              []Article
	GeneratedAt, LocaleName, CanonicalURL string
}

var whitespacePattern = regexp.MustCompile(`\s+`)

func cleanText(value string) string {
	value = strings.TrimSpace(value)
	return whitespacePattern.ReplaceAllString(value, " ")
}

func truncateText(value string, maxRunes int) string {
	value = cleanText(value)

	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}

	runes := []rune(value)

	if maxRunes == 1 {
		return "…"
	}

	return strings.TrimSpace(string(runes[:maxRunes-1])) + "…"
}

func firstText(document *goquery.Document, selectors ...string) string {
	for _, selector := range selectors {
		text := cleanText(document.Find(selector).First().Text())
		if text != "" {
			return text
		}
	}

	return ""
}

func acceptLanguage(locale string) string {
	switch locale {
	case "T":
		return "pt-BR,pt;q=0.9"
	case "S":
		return "es,es-ES;q=0.9"
	case "E":
		return "en,en-US;q=0.9"
	default:
		return "*"
	}
}

func enrichArticle(
	ctx context.Context,
	article *Article,
	locale string,
) error {
	wolURL, err := buildWOLURL(locale, article.ID)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		wolURL,
		nil,
	)
	if err != nil {
		return fmt.Errorf(
			"crear petición para %s: %w",
			article.ID,
			err,
		)
	}

	request.Header.Set(
		"User-Agent",
		"FeedAwake/1.0",
	)
	request.Header.Set(
		"Accept",
		"text/html,application/xhtml+xml",
	)
	request.Header.Set(
		"Accept-Language",
		acceptLanguage(locale),
	)

	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf(
			"descargar artículo %s: %w",
			article.ID,
			err,
		)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"descargar artículo %s: el servidor respondió %s",
			article.ID,
			response.Status,
		)
	}

	limitedBody := io.LimitReader(
		response.Body,
		2<<20,
	)

	document, err := goquery.NewDocumentFromReader(limitedBody)
	if err != nil {
		return fmt.Errorf(
			"analizar artículo %s: %w",
			article.ID,
			err,
		)
	}

	title := firstText(
		document,
		"#p1",
		`[data-pid="1"]`,
		"main h1",
		"article h1",
		"h1",
	)

	excerpt := firstText(
		document,
		"#p2",
		`[data-pid="2"]`,
		"main article p",
		"article p",
		"main p",
	)

	if title == "" {
		title = metaContent(
			document,
			`meta[property="og:title"]`,
		)
	}

	if excerpt == "" {
		excerpt = metaContent(
			document,
			`meta[property="og:description"]`,
		)
	}

	if title == "" {
		return fmt.Errorf(
			"el artículo %s no contiene un título reconocible",
			article.ID,
		)
	}

	article.Title = truncateText(title, 110)
	article.Excerpt = truncateText(excerpt, 260)
	article.WOLURL = wolURL

	return nil
}

func enrichArticles(
	ctx context.Context,
	articles []Article,
	locale string,
) {
	for index := range articles {
		err := enrichArticle(ctx, &articles[index], locale)
		if err != nil {
			fmt.Fprintf(
				os.Stderr,
				"advertencia: no se pudieron obtener metadatos de %s: %v\n",
				articles[index].ID,
				err,
			)

			applyArticleFallback(&articles[index], locale)
		}
	}
}

func applyArticleFallback(article *Article, locale string) {
	switch locale {
	case "T":
		article.Title = "Artigo da revista Despertai!"
		article.Excerpt = "Abra o artigo para descobrir esta leitura do arquivo."
	case "E":
		article.Title = "Awake! magazine article"
		article.Excerpt = "Open the article to discover this selection from the archive."
	default:
		article.Title = "Artículo de la revista ¡Despertad!"
		article.Excerpt = "Abre el artículo para descubrir esta lectura del archivo."
	}

	if article.WOLURL == "" {
		article.WOLURL, _ = buildWOLURL(locale, article.ID)
	}
}

func buildWOLURL(locale, documentID string) (string, error) {
	config, ok := wolLocales[locale]
	if !ok {
		return "", fmt.Errorf("locale WOL no soportado: %q", locale)
	}

	return fmt.Sprintf(
		"https://wol.jw.org/%s/wol/d/%s/%s/%s",
		config.Language,
		config.Region,
		config.Library,
		documentID,
	), nil
}

func Generate(c Config) error {
	if err := validate(c); err != nil {
		return err
	}
	bases, err := readBases(c.CSVPath, c.MinYear, c.MaxYear)
	if err != nil {
		return err
	}

	articles, err := chooseArticles(
		bases,
		c.Count,
		c.ArticlesPerIssue,
		c.Locale,
	)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		45*time.Second,
	)
	defer cancel()

	enrichArticles(ctx, articles, c.Locale)

	if err := os.MkdirAll(filepath.Dir(c.OutputPath), 0o755); err != nil {
		return fmt.Errorf("crear directorio de salida: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.OutputPath), ".feedawake-*.html")
	if err != nil {
		return fmt.Errorf("crear archivo temporal: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	data := PageData{Articles: articles, GeneratedAt: time.Now().UTC().Format("02 Jan 2006, 15:04 UTC"), LocaleName: allowedLocales[c.Locale], CanonicalURL: strings.TrimRight(c.SiteURL, "/")}
	if err := pageTemplate.Execute(tmp, data); err != nil {
		tmp.Close()
		return fmt.Errorf("renderizar HTML: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cerrar HTML: %w", err)
	}
	if err := os.Rename(tmpName, c.OutputPath); err != nil {
		return fmt.Errorf("publicar HTML: %w", err)
	}
	return nil
}

func validate(c Config) error {
	if _, ok := allowedLocales[c.Locale]; !ok {
		return fmt.Errorf("locale %q inválido: use S, T o E", c.Locale)
	}
	if c.Count < 1 || c.ArticlesPerIssue < 1 {
		return errors.New("count y articles-per-issue deben ser positivos")
	}
	if c.MinYear > c.MaxYear {
		return errors.New("min-year no puede superar max-year")
	}
	return nil
}

func readBases(path string, minYear, maxYear int) ([]int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("abrir CSV: %w", err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	seen := map[int64]struct{}{}
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("leer CSV: %w", err)
		}
		for _, raw := range row {
			s := strings.TrimSpace(raw)
			if s == "" {
				continue
			}
			if len(s) != 9 || !strings.HasSuffix(s, "0") {
				continue
			}
			id, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				continue
			}
			year, err := strconv.Atoi(s[2:6])
			if err != nil || year < minYear || year > maxYear {
				continue
			}
			seen[id] = struct{}{}
		}
	}
	bases := make([]int64, 0, len(seen))
	for id := range seen {
		bases = append(bases, id)
	}
	sort.Slice(bases, func(i, j int) bool { return bases[i] < bases[j] })
	if len(bases) == 0 {
		return nil, errors.New("el CSV no contiene IDs base válidos para el rango solicitado")
	}
	return bases, nil
}

func chooseArticles(bases []int64, count, perIssue int, locale string) ([]Article, error) {
	capacity := len(bases) * perIssue
	if count > capacity {
		return nil, fmt.Errorf("se pidieron %d artículos, pero solo hay %d candidatos", count, capacity)
	}
	selected := make(map[int64]struct{}, count)
	out := make([]Article, 0, count)
	for len(out) < count {
		bi, err := randomInt(len(bases))
		if err != nil {
			return nil, err
		}
		n, err := randomInt(perIssue)
		if err != nil {
			return nil, err
		}
		id := bases[bi] + int64(n+1)
		if _, exists := selected[id]; exists {
			continue
		}
		selected[id] = struct{}{}
		s := strconv.FormatInt(id, 10)
		year, _ := strconv.Atoi(s[2:6])
		url := fmt.Sprintf("https://www.jw.org/finder?wtlocale=%s&docid=%d&srctype=wol&srcid=share", locale, id)
		out = append(out, Article{ID: s, URL: url, Year: year})
	}
	return out, nil
}

func randomInt(max int) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, fmt.Errorf("generar aleatorio seguro: %w", err)
	}
	return int(n.Int64()), nil
}

func metaContent(
	document *goquery.Document,
	selector string,
) string {
	value, exists := document.Find(selector).First().Attr("content")
	if !exists {
		return ""
	}

	return cleanText(value)
}

var pageTemplate = template.Must(
	template.New("page").Parse(`<!doctype html>
<html lang="es">
<head>
	<meta charset="utf-8">
	<meta
		name="viewport"
		content="width=device-width, initial-scale=1"
	>

	<title>FeedAwake | Lecturas del archivo</title>

	<meta
		name="description"
		content="Cinco lecturas aleatorias de Despertad entre 1980 y 2000, renovadas dos veces al día."
	>
	<meta name="theme-color" content="#183b4e">

	<meta property="og:type" content="website">
	<meta
		property="og:title"
		content="FeedAwake | Lecturas del archivo"
	>
	<meta
		property="og:description"
		content="Descubre cinco artículos aleatorios del archivo de Despertad."
	>

	{{if .CanonicalURL}}
	{{.CanonicalURL}}/
	<meta property="og:url" content="{{.CanonicalURL}}/">
	{{end}}

	<style>
		:root {
			color-scheme: light dark;

			--bg: #f4efe5;
			--bg-highlight: #fff8e8;
			--ink: #17313d;
			--card: #fffdf8;
			--accent: #cf6a3a;
			--accent-dark: #a94d27;
			--muted: #66777e;
			--border: rgba(23, 49, 61, 0.14);
			--shadow: rgba(23, 49, 61, 0.10);
			--shadow-hover: rgba(23, 49, 61, 0.20);
		}

		* {
			box-sizing: border-box;
		}

		html {
			scroll-behavior: smooth;
		}

		body {
			min-height: 100vh;
			margin: 0;
			font-family:
				Inter,
				ui-sans-serif,
				system-ui,
				-apple-system,
				BlinkMacSystemFont,
				"Segoe UI",
				sans-serif;
			color: var(--ink);
			background:
				radial-gradient(
					circle at 15% 5%,
					var(--bg-highlight) 0,
					transparent 35%
				),
				var(--bg);
		}

		.wrap {
			width: min(1120px, 92%);
			margin: auto;
			padding: 64px 0 40px;
		}

		.hero {
			max-width: 820px;
		}

		.eyebrow {
			margin-bottom: 18px;
			color: var(--accent);
			font-size: 0.78rem;
			font-weight: 800;
			letter-spacing: 0.16em;
			text-transform: uppercase;
		}

		h1 {
			margin: 0 0 24px;
			font-family: Georgia, "Times New Roman", serif;
			font-size: clamp(2.8rem, 7vw, 5.7rem);
			font-weight: 700;
			line-height: 0.96;
			letter-spacing: -0.04em;
		}

		.lead {
			max-width: 700px;
			margin: 0;
			color: var(--muted);
			font-size: 1.12rem;
			line-height: 1.7;
		}

		.grid {
			display: grid;
			grid-template-columns:
				repeat(auto-fit, minmax(min(100%, 310px), 1fr));
			gap: 22px;
			margin: 48px 0;
		}

		.card {
			position: relative;
			display: flex;
			min-height: 370px;
			flex-direction: column;
			justify-content: space-between;
			gap: 24px;
			padding: 27px;
			overflow: hidden;
			color: var(--ink);
			text-decoration: none;
			background: var(--card);
			border: 1px solid var(--border);
			border-radius: 24px;
			box-shadow: 0 12px 30px var(--shadow);
			transition:
				transform 180ms ease,
				box-shadow 180ms ease,
				border-color 180ms ease;
		}

		.card::before {
			position: absolute;
			inset: 0 auto 0 0;
			width: 6px;
			content: "";
			background: var(--accent);
			opacity: 0;
			transition: opacity 180ms ease;
		}

		.card:hover {
			transform: translateY(-6px);
			border-color: var(--accent);
			box-shadow: 0 20px 45px var(--shadow-hover);
		}

		.card:hover::before {
			opacity: 1;
		}

		.card:focus-visible {
			outline: 4px solid var(--accent);
			outline-offset: 5px;
		}

		.card-top {
			display: flex;
			align-items: flex-start;
			justify-content: space-between;
			gap: 16px;
		}

		.year {
			font-family: Georgia, "Times New Roman", serif;
			font-size: 2.8rem;
			font-weight: 700;
			line-height: 1;
			letter-spacing: -0.04em;
		}

		.id {
			padding: 7px 9px;
			color: var(--muted);
			font-family:
				ui-monospace,
				SFMono-Regular,
				Consolas,
				monospace;
			font-size: 0.72rem;
			background: rgba(23, 49, 61, 0.06);
			border-radius: 999px;
		}

		.card-content {
			flex: 1;
		}

		.card h2 {
			margin: 0 0 16px;
			font-family: Georgia, "Times New Roman", serif;
			font-size: 1.55rem;
			line-height: 1.22;
			letter-spacing: -0.015em;
			text-wrap: balance;
		}

		.card p {
			display: -webkit-box;
			margin: 0;
			overflow: hidden;
			color: var(--muted);
			font-size: 0.96rem;
			line-height: 1.65;
			-webkit-box-orient: vertical;
			-webkit-line-clamp: 6;
		}

		.card-actions {
			display: flex;
			align-items: center;
			justify-content: space-between;
			gap: 12px;
			padding-top: 18px;
			border-top: 1px solid var(--border);
		}

		.open {
			color: var(--accent);
			font-weight: 800;
		}

		.arrow {
			display: inline-grid;
			width: 38px;
			height: 38px;
			place-items: center;
			color: #fff;
			font-size: 1.1rem;
			background: var(--accent);
			border-radius: 50%;
			transition:
				transform 180ms ease,
				background 180ms ease;
		}

		.card:hover .arrow {
			transform: translate(2px, -2px);
			background: var(--accent-dark);
		}

		footer {
			display: flex;
			justify-content: space-between;
			gap: 20px;
			flex-wrap: wrap;
			padding-top: 25px;
			color: var(--muted);
			font-size: 0.85rem;
			border-top: 1px solid var(--border);
		}

		@media (max-width: 600px) {
			.wrap {
				padding-top: 42px;
			}

			.grid {
				margin-top: 36px;
			}

			.card {
				min-height: 340px;
			}

			.year {
				font-size: 2.4rem;
			}
		}

		@media (prefers-reduced-motion: reduce) {
			html {
				scroll-behavior: auto;
			}

			.card,
			.card::before,
			.arrow {
				transition: none;
			}

			.card:hover {
				transform: none;
			}
		}

		@media (prefers-color-scheme: dark) {
			:root {
				--bg: #102630;
				--bg-highlight: #183b4e;
				--ink: #f8f1e5;
				--card: #183b4e;
				--muted: #bdcbd0;
				--border: rgba(255, 255, 255, 0.10);
				--shadow: rgba(0, 0, 0, 0.18);
				--shadow-hover: rgba(0, 0, 0, 0.32);
			}

			.id {
				background: rgba(255, 255, 255, 0.07);
			}
		}
	</style>
</head>

<body>
	<main class="wrap">
		<header class="hero">
			<div class="eyebrow">
				Archivo 1980–2000 · {{.LocaleName}}
			</div>

			<h1>
				Cinco lecturas<br>
				para hoy.
			</h1>

			<p class="lead">
				Una selección aleatoria del archivo de
				<em>Despertad</em>.
				La portada se renueva automáticamente dos veces al día.
			</p>
		</header>

		<section
			class="grid"
			aria-label="Artículos seleccionados"
		>
			{{range .Articles}}
			<a href="{{.URL}}">
				<div class="card-top">
					<span class="year">{{.Year}}</span>
					<span class="id">docid {{.ID}}</span>
				</div>

				<div class="card-content">
					<h2>{{.Title}}</h2>

					{{if .Excerpt}}
					<p>{{.Excerpt}}</p>
					{{end}}
				</div>

				<div class="card-actions">
					<span class="open">Leer artículo</span>

					<span class="arrow" aria-hidden="true">
						↗
					</span>
				</div>
			</a>
			{{end}}
		</section>

		<footer>
			<span>Actualizado {{.GeneratedAt}}</span>
			<span>FeedAwake · selección automática</span>
		</footer>
	</main>
</body>
</html>`),
)
