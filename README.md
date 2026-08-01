# FeedAwake

Generador estático en Go que elige cinco artículos de números de *Despertad* entre 1980 y 2000 y publica una portada mediante GitHub Pages.

## Regla de IDs

El CSV contiene IDs de sumarios terminados en `0`. FeedAwake nunca enlaza esos IDs. Para cada selección toma un sumario y agrega un número entre `1` y `13`. Por ejemplo, desde `101990240` puede producir `101990241`.

## Uso local

```bash
go test ./...
go run ./cmd/feedawake -locale S -count 5 -out docs/index.html
```

Locales: `S` español, `T` portugués y `E` inglés.

## GitHub Pages

1. Sube el proyecto a un repositorio GitHub.
2. En **Settings > Pages > Build and deployment > Source**, selecciona **GitHub Actions**.
3. Opcionalmente crea la variable de repositorio `SITE_URL`, por ejemplo `https://usuario.github.io/feedawake`, para emitir `canonical` y `og:url`.
4. Ejecuta manualmente el workflow una vez o espera al horario programado.

El workflow corre a las 09:17 y 21:17 UTC, aproximadamente 06:17 y 18:17 en Paraguay (UTC-3), y también admite ejecución manual.

## Opciones

```text
-csv                  CSV de números (default awake1980-2000.csv)
-out                  HTML de salida (default docs/index.html)
-locale               S, T o E (default S)
-count                cantidad de tarjetas (default 5)
-articles-per-issue   máximo estimado por número (default 13)
-min-year / -max-year rango permitido (default 1980..2000)
-site-url             URL pública canónica opcional
```

## Decisiones técnicas

- `crypto/rand` evita semillas repetibles.
- Selección sin IDs duplicados.
- Filtro estricto por año e ID base terminado en cero.
- Escritura atómica mediante archivo temporal y `rename`.
- HTML escapado con `html/template`.
- Despliegue oficial de Pages sin commits automáticos.
