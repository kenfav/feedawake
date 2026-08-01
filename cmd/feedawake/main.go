package main

import (
    "flag"
    "fmt"
    "log"
    "os"

    "github.com/kpfav/feedawake/internal/feed"
)

func main() {
    var cfg feed.Config
    flag.StringVar(&cfg.CSVPath, "csv", "awake1980-2000.csv", "ruta del CSV de números")
    flag.StringVar(&cfg.OutputPath, "out", "docs/index.html", "archivo HTML de salida")
    flag.StringVar(&cfg.Locale, "locale", "T", "idioma JW: T portugués, E inglés, S español")
    flag.IntVar(&cfg.Count, "count", 5, "cantidad de artículos")
    flag.IntVar(&cfg.ArticlesPerIssue, "articles-per-issue", 13, "máximo estimado de artículos por número")
    flag.IntVar(&cfg.MinYear, "min-year", 1979, "año mínimo")
    flag.IntVar(&cfg.MaxYear, "max-year", 2000, "año máximo")
    flag.StringVar(&cfg.SiteURL, "site-url", os.Getenv("SITE_URL"), "URL pública canónica opcional")
    flag.Parse()

    if err := feed.Generate(cfg); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Feed generado en %s\n", cfg.OutputPath)
}
