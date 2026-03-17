# CPN Tool Engine — Spec Driven Design
> Versión 0.1 — Smart Tool Engine para AI Agent OS

---

## 1. Visión del Sistema

Un motor de ejecución que recibe una **Colored Petri Net** definida en código o JSON, la valida, y la ejecuta concurrentemente — disparando transitions cuando sus places de entrada tienen tokens disponibles, hasta que la red alcanza un estado final o se detiene por error.

---

## 2. Alcance de esta Spec (v0.1)

| Incluido | Excluido (futuras versiones) |
|---|---|
| Definición de CPN en Go structs | Smart Planner (diseño JIT de CPNs) |
| Executor con firing rule | Tool Latent Space |
| Paralelismo por goroutines | Persistencia y feedback loop |
| Color sets básicos | Editor visual |
| Tests de cada spec | Integración con LLM |

---

## 3. Color Sets

### Spec 3.1 — Definición de ColorSet
```
DADO que el sistema necesita tipar los tokens
CUANDO se define un ColorSet
ENTONCES debe ser uno de: STRING, JSON, ARTIFACT, SCORE
```

### Spec 3.2 — Validación de tipo
```
DADO un Place con ColorSet JSON
CUANDO llega un Token con Color STRING
ENTONCES el sistema debe rechazar el token con error ErrColorMismatch
```

---

## 4. Token

### Spec 4.1 — Estructura mínima
```
DADO que un Token transporta datos entre places
CUANDO se crea un Token
ENTONCES debe tener: Color ColorSet, Payload any
```

### Spec 4.2 — Token nulo
```
DADO un Place vacío
CUANDO se intenta consumir un Token
ENTONCES debe retornar error ErrEmptyPlace
```

---

## 5. Place

### Spec 5.1 — Identificación única
```
DADO que un Place es un buffer con identidad
CUANDO se crea un Place
ENTONCES su ID debe ser único dentro de la CPN
```

### Spec 5.2 — Depósito de token
```
DADO un Place con ColorSet JSON
CUANDO se deposita un Token JSON
ENTONCES len(place.Tokens) debe incrementar en 1
```

### Spec 5.3 — Rechazo de color incorrecto
```
DADO un Place con ColorSet STRING
CUANDO se deposita un Token JSON
ENTONCES debe retornar ErrColorMismatch y Tokens no debe cambiar
```

### Spec 5.4 — Marcado inicial
```
DADO una CPN con marcado inicial definido
CUANDO se inicializa la CPN
ENTONCES cada Place debe tener exactamente los tokens del marcado inicial
```

---

## 6. Transition

### Spec 6.1 — Estructura mínima
```
DADO que una Transition representa una tool ejecutable
CUANDO se define una Transition
ENTONCES debe tener:
  - ID string (único)
  - InputPlaces  []string
  - OutputPlaces []string
  - ToolName     string
  - Executor     func(Token) (Token, error)
```

### Spec 6.2 — Firing rule
```
DADO una Transition con InputPlaces ["P:TICKER", "P:QUERY"]
CUANDO P:TICKER tiene 1 token y P:QUERY tiene 0 tokens
ENTONCES CanFire() debe retornar false
```

### Spec 6.3 — Firing rule satisfecha
```
DADO una Transition con InputPlaces ["P:TICKER"]
CUANDO P:TICKER tiene al menos 1 token
ENTONCES CanFire() debe retornar true
```

### Spec 6.4 — Consumo atómico
```
DADO una Transition que dispara
CUANDO consume tokens de InputPlaces
ENTONCES los tokens deben ser removidos ANTES de ejecutar el Executor
  (para evitar doble consumo en ejecución paralela)
```

### Spec 6.5 — Producción de tokens
```
DADO una Transition que ejecuta su Executor exitosamente
CUANDO el Executor retorna (Token, nil)
ENTONCES el Token debe depositarse en cada OutputPlace
```

### Spec 6.6 — Manejo de error en Executor
```
DADO una Transition cuyo Executor retorna (_, error)
CUANDO ocurre el error
ENTONCES la CPN debe transicionar a estado FAILED
  y el error debe estar disponible en CPN.Error
```

---

## 7. CPN (Red completa)

### Spec 7.1 — Estructura
```
DADO que una CPN es el grafo completo
CUANDO se define una CPN
ENTONCES debe tener:
  - Places      map[string]*Place
  - Transitions map[string]*Transition
  - Estado      CPNState (IDLE, RUNNING, COMPLETED, FAILED)
  - Error       error
```

### Spec 7.2 — Validación de arcos
```
DADO una Transition con InputPlaces ["P:INEXISTENTE"]
CUANDO se valida la CPN antes de ejecutar
ENTONCES debe retornar ErrInvalidArc
```

### Spec 7.3 — Detección de estado final
```
DADO una CPN en ejecución
CUANDO ninguna Transition puede disparar
  Y todos los OutputPlaces finales tienen tokens
ENTONCES el estado debe cambiar a COMPLETED
```

### Spec 7.4 — Detección de deadlock
```
DADO una CPN en ejecución
CUANDO ninguna Transition puede disparar
  Y el estado no es COMPLETED
ENTONCES el estado debe cambiar a FAILED con ErrDeadlock
```

---

## 8. Executor (Motor de ejecución)

### Spec 8.1 — Loop principal
```
DADO una CPN válida con marcado inicial
CUANDO se llama a cpn.Run()
ENTONCES el executor debe:
  1. Encontrar todas las Transitions donde CanFire() == true
  2. Disparar cada una en una goroutine separada
  3. Esperar a que todas completen
  4. Repetir hasta COMPLETED o FAILED
```

### Spec 8.2 — Paralelismo
```
DADO dos Transitions A y B donde CanFire() == true simultáneamente
CUANDO el executor corre
ENTONCES A y B deben ejecutarse concurrentemente (no secuencialmente)
```

### Spec 8.3 — Thread safety en Places
```
DADO múltiples goroutines depositando tokens en el mismo Place
CUANDO ocurren escrituras concurrentes
ENTONCES no debe haber race conditions (usar sync.Mutex en Place)
```

### Spec 8.4 — Timeout
```
DADO una CPN en ejecución
CUANDO el tiempo total supera un timeout configurado
ENTONCES debe retornar ErrTimeout y estado FAILED
```

---

## 9. Errores definidos

```go
var (
    ErrColorMismatch = errors.New("token color does not match place color set")
    ErrEmptyPlace    = errors.New("no tokens available in place")
    ErrInvalidArc    = errors.New("transition references non-existent place")
    ErrDeadlock      = errors.New("no transitions can fire but CPN is not complete")
    ErrTimeout       = errors.New("CPN execution exceeded timeout")
)
```

---

## 10. Estructura de paquetes sugerida

```
cpn/
├── colors.go        // ColorSet, Token
├── place.go         // Place + métodos Deposit, Consume
├── transition.go    // Transition + CanFire, Fire
├── cpn.go           // CPN + Validate, Run
├── executor.go      // loop principal, goroutines
├── errors.go        // errores tipados
└── cpn_test.go      // tests por spec
```

---

## 11. Red de referencia — AI Tool Engine

```
INPUT ──► [planner] ──► P:TICKER ──► [get_fin] ──► P:FIN_DATA ──► [chart_gen] ──► P:CHART ──┐
                   └──► P:QUERY  ──► [web_search] ──► P:WEB→sent ──► [sentiment] ──► P:SENTIMENT ──┤──► [report] ──► OUTPUT
                                                  └──► P:WEB→summ ──► [summarizer] ──► P:SUMMARY ──┘
```

```go
net := &cpn.CPN{
    Places: map[string]*cpn.Place{
        "P:INPUT":      {ID: "P:INPUT",      Color: cpn.ColorString},
        "P:TICKER":     {ID: "P:TICKER",     Color: cpn.ColorJSON},
        "P:QUERY":      {ID: "P:QUERY",      Color: cpn.ColorJSON},
        "P:FIN_DATA":   {ID: "P:FIN_DATA",   Color: cpn.ColorJSON},
        "P:WEB_SENT":   {ID: "P:WEB_SENT",   Color: cpn.ColorJSON},
        "P:WEB_SUMM":   {ID: "P:WEB_SUMM",   Color: cpn.ColorJSON},
        "P:CHART":      {ID: "P:CHART",      Color: cpn.ColorArtifact},
        "P:SENTIMENT":  {ID: "P:SENTIMENT",  Color: cpn.ColorScore},
        "P:SUMMARY":    {ID: "P:SUMMARY",    Color: cpn.ColorArtifact},
        "P:OUTPUT":     {ID: "P:OUTPUT",     Color: cpn.ColorArtifact},
    },
    Transitions: map[string]*cpn.Transition{
        "planner": {
            ID:           "planner",
            InputPlaces:  []string{"P:INPUT"},
            OutputPlaces: []string{"P:TICKER", "P:QUERY"},
            ToolName:     "planner",
            Executor:     plannerTool,
        },
        // ── Parallel 1 ──────────────────────────────────────────
        "get_fin": {
            ID:           "get_fin",
            InputPlaces:  []string{"P:TICKER"},
            OutputPlaces: []string{"P:FIN_DATA"},
            ToolName:     "get_financials",
            Executor:     getFinTool,
        },
        "web_search": {
            ID:           "web_search",
            InputPlaces:  []string{"P:QUERY"},
            OutputPlaces: []string{"P:WEB_SENT", "P:WEB_SUMM"},
            ToolName:     "web_search",
            Executor:     webSearchTool,
        },
        // ── Parallel 2 ──────────────────────────────────────────
        "chart_gen": {
            ID:           "chart_gen",
            InputPlaces:  []string{"P:FIN_DATA"},
            OutputPlaces: []string{"P:CHART"},
            ToolName:     "chart_generator",
            Executor:     chartGenTool,
        },
        "sentiment": {
            ID:           "sentiment",
            InputPlaces:  []string{"P:WEB_SENT"},
            OutputPlaces: []string{"P:SENTIMENT"},
            ToolName:     "sentiment_analysis",
            Executor:     sentimentTool,
        },
        "summarizer": {
            ID:           "summarizer",
            InputPlaces:  []string{"P:WEB_SUMM"},
            OutputPlaces: []string{"P:SUMMARY"},
            ToolName:     "summarizer",
            Executor:     summarizerTool,
        },
        // ── Final ───────────────────────────────────────────────
        "report": {
            ID:           "report",
            InputPlaces:  []string{"P:CHART", "P:SENTIMENT", "P:SUMMARY"},
            OutputPlaces: []string{"P:OUTPUT"},
            ToolName:     "report_builder",
            Executor:     reportTool,
        },
    },
}

// Marcado inicial
net.Places["P:INPUT"].Tokens = []cpn.Token{
    {Color: cpn.ColorString, Payload: "Investiga Tesla, analiza finanzas y genera reporte ejecutivo"},
}

// Ejecutar
err := net.Run(context.WithTimeout(ctx, 60*time.Second))
```

---

## 12. Orden de implementación sugerido

1. `colors.go` + `errors.go` — tipos base
2. `place.go` — Deposit, Consume con mutex
3. `transition.go` — CanFire, Fire
4. `cpn.go` — Validate
5. `executor.go` — Run loop
6. `cpn_test.go` — un test por spec

