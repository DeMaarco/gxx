# Evaluaciones de desarrollo

`gxx-eval` es una herramienta del repositorio. Reutiliza la sesión, el bucle, las herramientas y los proveedores de producción. Cada intento usa un workspace temporal nuevo; solo carga las skills de la fixture. El modelo no recibe los evaluadores ni el guion de simulación.

## Ejecutar sin consumir cuota de modelos

```sh
go run ./cmd/gxx-eval --trials 1 --out eval-results-smoke
go run ./cmd/gxx-eval --matrix evals/matrix-eco.json --out eval-results-eco
go test -race ./test/...
go vet ./...
```

Los 24 casos cubren investigación, edición, web, Eco, compactación y permisos. La simulación usa herramientas reales con respuestas programadas: comprueba la infraestructura, no la calidad de un modelo ni su ahorro real. Las pruebas de proveedores utilizan servidores HTTP locales. CI ejecuta únicamente estas pruebas sin llamadas pagadas, en Linux, macOS y Windows.

El caso de navegador requiere `agent-browser` y `node` ya instalados. Comprueba el enlace Contact en una sesión propia y la cierra dentro de la misma invocación. Si falta una dependencia se registra como no ejecutado; si el comando falla se registra como fallo. No se instalan navegadores automáticamente.

## Comparar modelos con presupuesto

Las llamadas reales son manuales y utilizan las credenciales configuradas en gxx. No pongas claves ni tokens en las fixtures o matrices.

```sh
go run ./cmd/gxx-eval --live --matrix evals/matrix-models.json --max-tokens 300000 --max-requests 100 --out eval-results-baseline
go run ./cmd/gxx-eval --live --matrix evals/matrix-models.json --max-tokens 300000 --max-requests 100 --baseline eval-results-baseline/report.json --out eval-results-candidate
```

Estos comandos son ejemplos bajo demanda. La matriz debe contener modelos disponibles en tus cuentas. `matrix-eco.json` cambia únicamente Eco; `matrix-models.json` compara modelo y esfuerzo. Se conserva la configuración efectiva en el informe y no se cambia el modelo predeterminado ni la configuración guardada.

Por defecto se ejecutan tres intentos por caso y configuración, secuencialmente. `--case-timeout` limita cada caso a dos minutos y `--max-steps` a 24 pasos por turno. `--live` exige `--max-tokens` y `--max-requests` positivos. Se contabilizan también resúmenes y reintentos.

El límite de tokens se comprueba entre peticiones: la respuesta en curso puede superar el saldo restante. Las peticiones fallidas pueden tener consumo no comunicado. El coste en USD es una estimación con tarifas locales, no un límite monetario ni la factura de una suscripción OAuth. El coste desconocido no se convierte en cero.

## Informes y regresiones

`--out` debe señalar un directorio nuevo. Se generan `report.json` y `report.md` con revisión del código, hash de suite, configuración, comprobaciones, respuestas, consumo, compactaciones, duración y coste estimado. `--trace` añade metadatos de herramientas, sin argumentos, resultados completos ni razonamiento. Los directorios `eval-results*` están ignorados por Git.

Los estados son `passed`, `failed`, `review_required`, `interrupted` y `skipped`. Una rúbrica humana pendiente no cuenta como éxito. Las interrupciones y las dependencias ausentes se distinguen de un fallo del modelo. El código de salida 1 indica fallo, interrupción, falta de credenciales/presupuesto o ausencia total de intentos; 2 indica argumentos inválidos y 130 cancelación. Los informes se guardan también tras una interrupción.

Compara siempre la misma suite, configuración y cantidad de intentos completados. El informe separa calidad, coste y tiempo; una simulación o una muestra pequeña no demuestra una mejora real. `+dirty` indica que la revisión incluía cambios locales sin commit.

Para añadir una regresión, copia `case-template.json` y añade el caso a `suite.json`. Define archivos iniciales, modo, permisos, peticiones y criterios verificables. `git` e `initial_changes` permiten comprobar la conservación de cambios previos. Las comprobaciones de archivos, respuestas y comandos deben evaluar resultados; conserva los tests con `file_unchanged`. Usa `final_answer_contains` para continuidad y una `rubric` para aspectos que requieran revisión humana. El `script` sirve exclusivamente para probar la infraestructura sin modelo.

Las credenciales cargadas se redactan y no se registran cabeceras ni errores crudos del proveedor. Las respuestas pueden incluir contenido de las fixtures: usa datos sintéticos y revisa los informes antes de compartirlos. No se suben resultados automáticamente. Consulta [la referencia completa de opciones y esquema](README.md) para ampliar la suite.
