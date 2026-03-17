# Agentic OS — Spec Driven Development

> Documento de referencia para implementación en Go con Claude Code.  
> Todo lo aquí descrito fue diseñado mediante sesión socrática antes de escribir una línea de código.

---

## 0. Documentos relacionados

| Documento | Descripción | Estado |
|---|---|---|
| **Agentic OS — Spec Driven Development** (este documento) | Arquitectura general, capas, domain types, Orchestrator, Directors, Agent Pool | v0.1 |
| **CPN Tool Engine — Spec Driven Design** | Motor CPN: Color sets, Places, Transitions, Executor, red de referencia | v0.1 |

Ambos documentos son complementarios. El CPN Tool Engine es el motor de razonamiento interno de cada agente. Este documento describe cómo los agentes viven, se coordinan y se comunican.

---

## 1. Glosario de términos

Términos que emergieron por intuición durante el diseño y su nombre técnico formal.

| Término en el sistema | Nombre técnico formal | Origen |
|---|---|---|
| "el agente que lo ve todo" | **Orchestrator** | Intuición: "debe haber alguien que sepa que dos agentes están hablando" |
| "árbol con comunicación lateral" | **DAG con aristas de dominio** | Intuición: "un árbol no alcanza, necesito que hablen entre ellos" |
| "puertas abiertas entre Directors" | **Authorized lateral channels** | Intuición: "como en una startup, hablan directo" |
| "clonar un agente para dos teams" | **Prototype Pattern + Worker Pool** | Intuición: "que se cree una copia con su propio contexto" |
| "el agente se copia al ser usado" | **fork()** | Intuición: mismo concepto que fork() en Unix |
| "teams que se arman y se apagan" | **Ephemeral Teams** | Intuición: "los construye, los usa y los apaga" |
| "agentes disponibles para prestar" | **Agent Pool** | Intuición: "pertenecen al pool, los teams los toman prestado" |
| "resumen interno con marca de agente" | **Thinking blocks con agent_id** | Intuición: "pensamiento interno asociado a quien lo generó" |
| "flujos que aprenden de sí mismos" | **Pattern Mining + Flow Crystallization** | Intuición: "si se repite suficiente, que se vuelva predefinido" |
| "flujos que viajan entre clientes" | **Cross-client Flow Replication** | Intuición: "si funciona en un cliente, replicarlo en otros" |
| "el humano que desbloquea" | **Human in the Loop (HITL)** | Intuición: "el Orchestrator espera lo que diga el humano" |
| "ID de sesión que sabe el canal" | **Opaque session ID + context enrichment** | Intuición: "a través del session ID podría saber esa info" |
| "flujo que mide eficiencia" | **Flow Ranking by token/node efficiency** | Intuición: "si usan menos nodos y consiguen lo mismo, lo quiero ver" |
| "gerencia de dominio" | **Director** | Intuición: "dirección de dominio, no VP corporativo" |

---

## 2. Visión del sistema

Un **sistema operativo agéntico** que corre sobre Linux, accesible vía chat multicanal (web, WhatsApp, Telegram), que permite a pequeñas empresas interactuar con un agente que orquesta equipos dinámicos de sub-agentes especializados para resolver tareas de negocio.

### Dos modos de deployment

| Modo | Descripción |
|---|---|
| On-premise | El cliente instala el sistema en su propia máquina. Administra sus propios datos. |
| SaaS | El vendor administra la infraestructura. El cliente consume via chat. |

### El chat es la única interfaz

El usuario nunca sabe qué modo de operación se activó internamente. Solo ve el chat.

---

## 3. Tipos de operación

El sistema detecta automáticamente el tipo de tarea y activa el modo correspondiente.

### Tipo 1 — Simple / Conversacional
- Ejemplos: "revisa mis emails", "¿cuánto vendí ayer?"
- Flujo: Orchestrator resuelve directamente — LLM call con historial + tools → CPN mínima → stream al cliente
- Sin Directors. Sin ephemeral teams. Sin HITL obligatorio.
- El Orchestrator es el agente de primer nivel para este tipo.

### Tipo 2 — Operación de negocio
- Ejemplos: "agrega este producto a mi catálogo"
- Flujo: Director routing → ephemeral team → ejecuta → informa
- HITL opcional según umbral configurado por cliente.

### Tipo 3 — Cambio estructural
- Ejemplos: "necesito soportar productos de terceros proveedores"
- Flujo: agente genera SPEC → HITL aprueba → entra a SDLC → Director de Engineering coordina implementación
- Nunca ejecuta cambios estructurales sin aprobación humana explícita.

---

## 4. Arquitectura de capas

```
┌─────────────────────────────────────────────────────────┐
│                    HUMAN (HITL)                         │
│         interrupts · aprueba specs · decide bloqueos    │
└─────────────────────────┬───────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────┐
│                    ORCHESTRATOR                         │
│    escucha Event Store · consolida · stream al cliente  │
│    una goroutine por sesión · soporta stream responses  │
└─────────────────────────┬───────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────┐
│                  INTENT CLASSIFIER                      │
│           { type, domain, requires_hitl }               │
│         Tipo 1: simple · Tipo 2: business_op            │
│              Tipo 3: structural_change                  │
└─────────────────────────┬───────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────┐
│                   DIRECTOR LAYER                        │
│   Director Engineering · Director Marketing · Director Data · Director ···  │
│   lateral channels autorizados entre Directors          │
│   construye ephemeral teams en tiempo de ejecución      │
└─────────────────────────┬───────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────┐
│                    AGENT POOL                           │
│         prototype → fork() → instancia con contexto    │
│   DataEngineer · DevOps · Backend · Analyst · ···       │
│   un agente puede estar en múltiples teams simultáneos  │
└─────────────────────────┬───────────────────────────────┘
                          │
         ┌────────────────┴─────────────────┐
         ▼                                  ▼ async
┌────────────────────┐          ┌───────────────────────┐
│   SESSION STORE    │          │     EVENT STORE        │
│  [visible]         │          │  stream completo       │
│  [thinking/agent]  │          │  trazabilidad total    │
└────────┬───────────┘          └───────────┬───────────┘
         │                                  │
         └──────────────┬───────────────────┘
                        ▼
┌─────────────────────────────────────────────────────────┐
│             FLOW INTELLIGENCE ENGINE                    │
│   pattern mining · ranking · cross-client replication   │
└─────────────────────────┬───────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────┐
│           COLOURED PETRI NET ENGINE                     │
│       flows cristalizados · parallelism · tokens        │
└─────────────────────────┬───────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────┐
│                  LINUX SANDBOX                          │
│       usuario dedicado sin sudo · ls · cat · cd ···     │
└─────────────────────────────────────────────────────────┘
```

---

## 5. Domain types — Go structs

### 5.1 Canal y sesión

```go
type ChannelType string

const (
    ChannelWeb      ChannelType = "web"
    ChannelWhatsApp ChannelType = "whatsapp"
    ChannelTelegram ChannelType = "telegram"
)

// Session ID es opaco (UUID v4) — sin información embebida.
// Toda la riqueza del contexto vive en el struct, no en el ID.
// Patrón: opaque session ID + context enrichment.
type Session struct {
    ID        string
    UserID    string
    Channel   ChannelType
    Stream    chan StreamChunk
    History   []Message
    CreatedAt time.Time
}

type StreamChunk struct {
    SessionID string
    AgentID   string
    Content   string
    Done      bool
}
```

### 5.2 Mensajes e historial

```go
type MessageRole string

const (
    RoleUser      MessageRole = "user"
    RoleAssistant MessageRole = "assistant"
    // Thinking block — pensamiento interno con identidad de agente.
    // Enriquece el contexto de sesión sin exponerse al usuario.
    // Generado por el Director al disolver su ephemeral team.
    RoleThinking  MessageRole = "thinking"
)

type Message struct {
    ID            string
    Role          MessageRole
    Content       string
    AgentID       string      // quién generó este mensaje (vacío si es user)
    AgentName     string      // nombre legible del agente
    DirectorID    string      // Director que coordinó la acción
    TeamID        string      // ephemeral team que lo generó
    Timestamp     time.Time
}
```

### 5.3 Intent Classifier output

```go
type OperationType string

const (
    OpSimple           OperationType = "simple"
    OpBusinessOp       OperationType = "business_operation"
    OpStructuralChange OperationType = "structural_change"
)

// IntentResult — output del clasificador de intención.
// Tres campos, nada más. El Orchestrator decide el resto.
type IntentResult struct {
    Type         OperationType
    Domain       string  // "engineering" | "marketing" | "data" | "finance" | ...
    RequiresHITL bool
}
```

### 5.4 Agent Pool — prototype + fork()

```go
// AgentPrototype — el molde. Nunca ejecuta directamente.
// Patrón: Prototype Pattern + Worker Pool.
type AgentPrototype struct {
    ID           string
    Name         string
    Role         string
    SystemPrompt string
    Tools        []Tool
    MaxInstances int     // 0 = ilimitado
}

// AgentInstance — copia viva con contexto propio.
// Creada via fork() del prototipo. Contexto completamente aislado.
type AgentInstance struct {
    InstanceID  string
    PrototypeID string
    DirectorID  string
    TeamID      string
    SessionID   string
    Context     []Message
    CreatedAt   time.Time
}

type AgentPool struct {
    prototypes map[string]*AgentPrototype
    instances  map[string]*AgentInstance
    mu         sync.RWMutex
}
```

### 5.5 Ephemeral Teams

```go
type TeamStatus string

const (
    TeamActive    TeamStatus = "active"
    TeamDissolved TeamStatus = "dissolved"
)

// EphemeralTeam — se construye en tiempo de ejecución,
// coordina, ejecuta y se disuelve. Sus agentes regresan al pool.
type EphemeralTeam struct {
    ID         string
    SessionID  string
    DirectorID string
    Agents     []*AgentInstance
    Status     TeamStatus
    Goal       string
    CreatedAt  time.Time
    DoneAt     *time.Time
}
```

### 5.6 Director

```go
// Director — ownership de un dominio.
// Construye ephemeral teams, coordina lateral channels,
// emite thinking blocks al disolver su team.
type Director struct {
    ID     string
    Domain string
    pool   *AgentPool
    events chan<- Event
}

func (d *Director) BuildTeam(goal string, sessionID string) *EphemeralTeam {
    // fork() de prototipos según el goal
    // registra team en el pool
    // retorna team listo para coordinación
}

func (d *Director) Dissolve(team *EphemeralTeam) {
    // consolida resultados
    // emite EventTeamDissolved con thinking block
    // limpia instancias del pool
}
```

### 5.7 Eventos

```go
type EventType string

const (
    EventAgentCompleted  EventType = "agent_completed"
    EventAgentBlocked    EventType = "agent_blocked"
    EventTeamDissolved   EventType = "team_dissolved"
    EventHITLRequired    EventType = "hitl_required"
    EventLateralComm     EventType = "lateral_communication" // entre Directors
    EventStreamChunk     EventType = "stream_chunk"
)

// Event — append-only. Nunca se modifica un evento pasado.
// Lleva session_id para routing en el Orchestrator.
type Event struct {
    ID         string
    Type       EventType
    SessionID  string
    AgentID    string
    DirectorID string
    TeamID     string
    Payload    map[string]any
    Timestamp  time.Time
}
```

### 5.8 Orchestrator

```go
// Orchestrator — agente de primer nivel para operaciones simples.
// Escucha el Event Store para operaciones complejas.
// Hace stream al canal del usuario. Una goroutine por sesión activa.
type Orchestrator struct {
    sessions  map[string]*Session
    events    <-chan Event
    toolStore *ToolStore       // catálogo de tools disponibles
    llm       LLMClient        // cliente LLM para resolver operaciones simples
    mu        sync.RWMutex
}

// ResolveSimple — el Orchestrator actúa como agente directo.
// Construye contexto con historial + tools y llama al LLM.
func (o *Orchestrator) ResolveSimple(ctx context.Context, s *Session, input string) {
    context := o.buildContext(s)   // buffer limitado: thinking blocks + últimos N mensajes
    tools   := o.toolStore.Available(s.UserID)
    // LLM call → puede invocar tools via CPN mínima → stream al cliente
}

// buildContext — construye el buffer de historial para el LLM.
// Estrategia: thinking blocks como memoria comprimida + últimos N mensajes raw.
// Los thinking blocks contienen summaries de Directors anteriores —
// no se necesita historial raw completo.
func (o *Orchestrator) buildContext(s *Session) []Message {
    // thinking blocks (todos) + últimos N mensajes raw
}

func (o *Orchestrator) Run(ctx context.Context) {
    for {
        select {
        case event := <-o.events:
            go o.handle(event)
        case <-ctx.Done():
            return
        }
    }
}

func (o *Orchestrator) handle(e Event) {
    switch e.Type {
    case EventAgentCompleted:
        // consolida resultado en session store
    case EventAgentBlocked:
        // escala a HITL
    case EventTeamDissolved:
        // escribe thinking block con director_id + team_id al session store
    case EventHITLRequired:
        // notifica al canal del usuario y espera respuesta
    case EventLateralComm:
        // loggea comunicación lateral entre Directors — no interviene
    case EventStreamChunk:
        // enruta chunk al canal correcto del usuario
    }
}
```

### 5.9 Tool Store

```go
// ToolStore — catálogo de tools disponibles por usuario/cliente.
// El Orchestrator y los agentes consultan aquí qué tools pueden usar.
type ToolStore struct {
    tools map[string][]Tool  // user_id → tools disponibles
    mu    sync.RWMutex
}

type Tool struct {
    Name        string
    Description string
    ColorIn     cpn.ColorSet   // tipo de token de entrada
    ColorOut    cpn.ColorSet   // tipo de token de salida
    Executor    func(cpn.Token) (cpn.Token, error)
}
```

---

## 6. Intent Classifier — comportamiento esperado

### Input
Mensaje raw del usuario en texto libre.

### Output
```json
{
  "type": "simple | business_operation | structural_change",
  "domain": "engineering | marketing | data | finance | ...",
  "requires_hitl": true
}
```

### Reglas de clasificación
- `simple`: no modifica datos estructurales, respuesta en una sola pasada
- `business_operation`: modifica datos existentes, puede requerir coordinación entre Directors
- `structural_change`: requiere cambios en schema, infraestructura o código — **siempre** `requires_hitl: true`

### Implementación sugerida
LLM call con system prompt estricto que devuelve **solo JSON**, sin preamble ni markdown fences. Parsear con `json.Unmarshal` directo.

---

## 7. Director Layer — comportamiento esperado

### Responsabilidades
- Recibir tarea del Orchestrator con `IntentResult`
- Construir `EphemeralTeam` en tiempo de ejecución tomando instancias del `AgentPool` via fork()
- Coordinar agentes del team
- Autorizar lateral channels con otros Directors (puertas abiertas)
- Al terminar: disolver el team y emitir `EventTeamDissolved` con thinking block

### Lateral channels
Un Director puede comunicarse directamente con otro Director sin escalar al Orchestrator.  
El Orchestrator **siempre** recibe `EventLateralComm` via Event Store.  
El Orchestrator **no decide** en la comunicación lateral — solo observa y loggea.

### Directors predefinidos
| Director | Dominio |
|---|---|
| Director Engineering | infraestructura, código, DevOps, backend |
| Director Marketing | campañas, contenido, análisis de audiencia |
| Director Data | análisis, reportes, pipelines de datos |

Directors adicionales se definen en configuración, no en código hardcodeado.

---

## 8. Agent Pool — comportamiento esperado

### Fork semántico
Cuando un Director solicita un agente:
1. Se busca el `AgentPrototype` por nombre o rol
2. Se crea un `AgentInstance` con `InstanceID` nuevo
3. La instancia recibe su propio slice de `Context` — independiente de otras instancias del mismo prototipo
4. La instancia se registra en el pool con su `TeamID`, `DirectorID` y `SessionID`

### Superposición de instancias
Un mismo prototipo puede tener múltiples instancias activas simultáneamente en diferentes teams.  
Cada instancia tiene contexto completamente aislado.

### Ciclo de vida
```
Prototype → fork() → Instance [active] → work → Instance [done] → pool cleanup
```

---

## 9. Session Store — estructura del historial

Cada mensaje en el historial lleva identidad de agente:

```
role: "user"      → input del usuario
role: "assistant" → respuesta visible al usuario
role: "thinking"  → pensamiento interno, lleva agent_id + director_id + team_id
```

El `thinking` block es generado por el Director al disolver su ephemeral team. Resume qué hizo cada agente, qué encontró, y cómo se resolvió. Enriquece el contexto para turnos futuros sin exponer detalles internos al usuario.

---

## 10. Event Store — estructura de eventos

Cada evento lleva:
- `session_id` — para routing en el Orchestrator
- `agent_id` — trazabilidad por agente
- `director_id` — trazabilidad por Director
- `team_id` — trazabilidad por ephemeral team
- `timestamp` — orden cronológico garantizado

El Event Store es **append-only**. Nunca se modifican eventos pasados.

El Orchestrator es el único consumer que escribe al Session Store.  
El Flow Intelligence Engine consume el Event Store de forma asíncrona sin bloquear el flujo principal.

---

## 11. Orchestrator — dos modos de operación

### Modo 1 — Agente directo (operación simple)

```
usuario: "¿cuánto vendí ayer?"
        ↓
Intent Classifier → { type: simple }
        ↓
Orchestrator.ResolveSimple()
    ├── buildContext() → thinking blocks + últimos N mensajes raw
    ├── toolStore.Available(userID) → tools del cliente
    └── LLM call
            ↓
        LLM invoca tool → CPN mínima → resultado
            ↓
        stream al cliente
```

El Orchestrator no delega. Resuelve él mismo con el LLM y las tools disponibles.

### Modo 2 — Coordinador (operación compleja)

```
Agente / Director produce chunk o evento
        ↓
Event Store ← EventStreamChunk | EventTeamDissolved | ...
        ↓
Orchestrator recibe evento
        ↓
lookup session por session_id
        ↓
handle(event) según tipo
        ↓
switch session.Channel:
    web       → SSE (text/event-stream)
    whatsapp  → WhatsApp Business API
    telegram  → Telegram Bot API
        ↓
chunk o resultado entregado al cliente en tiempo real
```

El agente nunca conoce el canal. Solo produce contenido.  
El Orchestrator es el único que sabe cómo entregar cada respuesta.

### Buffer de historial — estrategia

```
historial completo de sesión
        ↓
buildContext():
    ├── todos los thinking blocks  ← memoria comprimida de operaciones pasadas
    └── últimos N mensajes raw     ← conversación reciente
        ↓
contexto enviado al LLM
```

Los thinking blocks con `agent_id` y `director_id` actúan como memoria comprimida — el Orchestrator no necesita el historial raw completo para tener contexto rico de lo que pasó antes.

---

## 12. Linux Sandbox

- Se crea un usuario de sistema dedicado (no sudoer)
- Los agentes que necesitan acceso al filesystem operan bajo ese usuario
- Herramientas disponibles: `ls`, `cd`, `cat`, `grep`, `find`, `echo`, `pwd`
- Sin acceso a `sudo`, `chmod` en directorios del sistema, ni instalación de paquetes
- El sandbox es el límite de seguridad natural — si el agente es comprometido, no puede escalar privilegios

---

## 13. Flow Intelligence Engine — comportamiento esperado

### Fase 1 (MVP)
- Registrar cada flujo ejecutado con métricas: nodos usados, tiempo total, tokens consumidos, resultado exitoso/fallido
- Ranking simple por: menos nodos + mismo resultado = mejor score

### Fase 2
- Pattern mining sobre Event Store: detectar secuencias repetidas entre sesiones
- Proponer flujos candidatos a cristalizar
- HITL aprueba antes de que un patrón se vuelva predefined flow

### Fase 3 — Cross-client replication
- Un flow probado en cliente A puede proponerse a cliente B con dominio similar
- Cada flow tiene métricas de adopción y performance
- El conocimiento de un cliente enriquece el sistema para todos

---

## 14. Coloured Petri Net Engine

> Spec completo en documento separado: **CPN Tool Engine — Spec Driven Design v0.1**

### Rol en la arquitectura

El CPN Engine **no es un componente al final del stack**. Es el **motor de razonamiento interno de cada agente**. Cada vez que un agente necesita ejecutar tools, genera una CPN just-in-time y la ejecuta.

```
Agente recibe tarea
        ↓
genera CPN just-in-time
        ↓
cpn.Run() → tools ejecutan con paralelismo emergente
        ↓
tokens convergen en nodo final
        ↓
output del agente
```

### Tres niveles de uso

| Nivel | Descripción | Ejemplo |
|---|---|---|
| **Agente individual** | CPN generada JIT para razonar y ejecutar tools | Data Engineer analiza codebase + requerimiento en paralelo, converge en Planner |
| **Tool call simple** | CPN mínima de 1-2 nodos para operación directa | "revisa mis emails" → CPN con un solo nodo |
| **Flow cristalizado** | CPN pre-diseñada y probada, generada por Flow Intelligence | Red compleja reutilizable entre sesiones y clientes |

### Ejemplo concreto — agente programador recibe issue de GitHub

```
P:ISSUE (STRING)
        ↓
   [planner]
        ├──► P:CODEBASE_QUERY ──► [analyze_codebase]  ──► P:CODE_CONTEXT  ──┐
        └──► P:REQ_QUERY      ──► [analyze_requirement]──► P:REQ_CONTEXT   ──┤
                                                                              ▼
                                                                         [impl_planner]
                                                                              ↓
                                                                      P:IMPL_PLAN (ARTIFACT)
```

`analyze_codebase` y `analyze_requirement` corren en paralelo — paralelismo emergente de la topología, no declarado explícitamente.

### Color sets disponibles (del CPN spec)

| ColorSet | Uso típico |
|---|---|
| `STRING` | inputs de texto, queries |
| `JSON` | datos estructurados entre tools |
| `ARTIFACT` | outputs finales — reportes, planes, código |
| `SCORE` | resultados de análisis numérico |

### Contrato de integración con el Agentic OS

```go
// El agente construye su red y la ejecuta
net := buildCPN(task)  // JIT — diseñada para esta tarea específica
ctx := context.WithTimeout(parentCtx, 60*time.Second)
err := net.Run(ctx)

// El resultado final vive en el place de output
output := net.Places["P:OUTPUT"].Tokens[0]

// El agente emite el resultado como evento
events <- Event{
    Type:      EventAgentCompleted,
    SessionID: session.ID,
    AgentID:   instance.InstanceID,
    Payload:   map[string]any{"output": output},
}
```

### Lo que el CPN Engine NO hace en este contexto
- No conoce sesiones, agentes ni Directors
- No emite eventos al Event Store directamente — el agente wrapper lo hace
- No persiste estado entre ejecuciones — cada `Run()` es independiente

---

## 15. Orden de implementación sugerido

```
Paso 1 — Domain types
         Todos los structs de la sección 5.
         Sin lógica, solo tipos. Compilar sin errores.

Paso 2 — Intent Classifier
         LLM call → JSON output → IntentResult
         Unit tests con casos de los 3 tipos de operación.

Paso 3 — Session Store
         CRUD básico de sesiones en memoria (luego persistir).
         Generación de session_id opaco (UUID v4).

Paso 4 — Event Store
         Canal de Go + append-only slice en memoria.
         Emisión y consumo básico de eventos.

Paso 5 — Orchestrator
         goroutine principal + dispatch por EventType.
         Stream básico hacia canal web (SSE).

Paso 6 — Agent Pool
         Registro de prototipos + fork() + cleanup.
         Test: mismo prototipo, dos instancias, contextos aislados.

Paso 7 — Director Layer
         Un Director hardcodeado (Director Engineering) como prueba.
         Construye ephemeral team, coordina, disuelve, emite thinking block.

Paso 8 — Linux Sandbox
         Crear usuario de sistema.
         Wrapper de Go para ejecutar comandos bajo ese usuario.

Paso 9 — Integración CPN Engine
         Conectar flows cristalizados con el Director Layer.

Paso 10 — Flow Intelligence Engine
          Fase 1: métricas y ranking básico.
```

---

## 16. Decisiones de diseño tomadas

| Decisión | Valor | Razón |
|---|---|---|
| Orchestrator como agente directo | Resuelve operaciones simples sin invocar Directors | Sin overhead de ephemeral teams para el 80% de interacciones |
| Buffer de historial | Thinking blocks completos + últimos N mensajes raw | Memoria comprimida rica sin saturar el context window |
| Tool Store | Catálogo de tools por usuario/cliente | El Orchestrator y los agentes consultan el mismo catálogo |
| Session ID | UUID opaco | Seguridad — sin info embebida en el ID |
| Lateral channels entre Directors | Permitidos (puertas abiertas) | Evita latencia de escalamiento innecesario |
| HITL en cambios estructurales | Siempre obligatorio | Gobernanza — el agente no toca producción sin aprobación |
| Contexto de agente por instancia | Aislado por fork() | Instancias simultáneas del mismo prototipo sin interferencia |
| Event Store | Append-only | Trazabilidad completa, reconstrucción posible |
| Flow Intelligence | Async, no bloquea flujo principal | El aprendizaje no penaliza la respuesta |
| Thinking blocks | Con agent_id + director_id + team_id | Saber exactamente qué agente pensó qué y en qué contexto |
| Nomenclatura | Director en lugar de VP | Técnica y semántica — dirección de dominio sin connotación corporativa |

---

## 17. Lo que este documento NO incluye (trabajo futuro)

- Persistencia: Session Store y Event Store en memoria por ahora. Redis / Postgres en fase siguiente.
- Autenticación de usuarios y multi-tenancy.
- Rate limiting por canal.
- Billing / metering por tokens consumidos.
- UI de administración para aprobar flows en HITL.
- Deployment: Docker, Kubernetes, configuración de usuario Linux en CI.
- CPN Engine spec completo (documento separado ya existe).

