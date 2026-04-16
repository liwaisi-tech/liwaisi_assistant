# Motor Agéntico CPN

Un patrón para construir motores de orquestación de agentes de IA utilizando Redes de Petri Coloreadas.

Este documento está diseñado para darle a cualquier ingeniero — desde fundador de startup hasta programador de sistemas — una comprensión profunda, desde los primeros principios, de cómo construir un motor de IA agéntica de grado de producción sobre Redes de Petri Coloreadas (CPNs). Es agnóstico al lenguaje, con ejemplos funcionales en Go, Rust y Elixir. El objetivo no es venderte un framework sino enseñarte las matemáticas, la arquitectura y la ingeniería para que puedas construir el tuyo propio.

La mayoría de los sistemas de IA agéntica hoy en día son pipelines imperativos disfrazados de "agentes". Llaman a un LLM, parsean la salida, llaman a otro LLM, tal vez usan una herramienta. Esto funciona para demos. Se desmorona cuando necesitas concurrencia, compuertas de aprobación humano-en-el-ciclo, composición dinámica de equipos, reintento con circuit breakers, streaming, seguimiento de costos, y la capacidad de verificar formalmente que tu agente no puede entrar en bloqueo (deadlock). Las Redes de Petri Coloreadas te dan todo eso, y la teoría ha sido probada en batalla durante 60 años en campos donde la corrección importa: plantas nucleares, aviónica y protocolos de telecomunicaciones.

La idea clave es esta: **cada agente, herramienta, coordinador e interacción humana es la misma cosa — una instancia de CPN cuya identidad emerge de su topología y profundidad en la jerarquía.** No hay clase `Agent`, ni clase `Tool`, ni clase `Coordinator`. Hay un único tipo universal. Esta simplificación radical es lo que hace al sistema tratable.

---

## Tabla de Contenidos

1. [¿Por qué Redes de Petri para Agentes?](#1-por-qué-redes-de-petri-para-agentes)
2. [Fundamentos Matemáticos](#2-fundamentos-matemáticos)
3. [De las Matemáticas al Código: Tipos Centrales](#3-de-las-matemáticas-al-código-tipos-centrales)
4. [Las Cinco Primitivas](#4-las-cinco-primitivas)
5. [Espacios de Comunicación](#5-espacios-de-comunicación)
6. [El Bucle Ejecutor](#6-el-bucle-ejecutor)
7. [Tipos de Transición: Los Seis Tipos de Nodo](#7-tipos-de-transición-los-seis-tipos-de-nodo)
8. [El Sistema de Modo Dual: MAS y Centauriano](#8-el-sistema-de-modo-dual-mas-y-centauriano)
9. [Humano-en-el-Ciclo (HITL)](#9-humano-en-el-ciclo-hitl)
10. [Sub-CPNs: Equipos Jerárquicos de Agentes](#10-sub-cpns-equipos-jerárquicos-de-agentes)
11. [Agentes de Grupo y Observación de Eventos](#11-agentes-de-grupo-y-observación-de-eventos)
12. [Memoria y Ensamblado de Contexto](#12-memoria-y-ensamblado-de-contexto)
13. [Reintento, Circuit Breakers y Resiliencia](#13-reintento-circuit-breakers-y-resiliencia)
14. [Validación y Autocorrección](#14-validación-y-autocorrección)
15. [Sesiones: La Única Interfaz del Usuario](#15-sesiones-la-única-interfaz-del-usuario)
16. [Validación de Topología](#16-validación-de-topología)
17. [Seguimiento de Costos y Ranking de Flujos](#17-seguimiento-de-costos-y-ranking-de-flujos)
18. [Arquitectura Hexagonal: Puertos y Adaptadores](#18-arquitectura-hexagonal-puertos-y-adaptadores)
19. [Construyendo tu Primer Agente CPN (Paso a Paso)](#19-construyendo-tu-primer-agente-cpn-paso-a-paso)
20. [Ejemplos Completos Funcionales](#20-ejemplos-completos-funcionales)
21. [Ruta de Aprendizaje y Recursos](#21-ruta-de-aprendizaje-y-recursos)

---

## 1. ¿Por qué Redes de Petri para Agentes?

El espacio de la IA agéntica tiene un problema de herramientas. La mayoría de los frameworks te dan una de dos cosas:

1. **DAGs** (Grafos Acíclicos Dirigidos) — buenos para pipelines estáticos, malos para bucles, interacción humana y comportamiento dinámico.
2. **Código imperativo con llamadas a LLM** — flexible pero imposible de razonar formalmente. No puedes probar la ausencia de bloqueos. No puedes visualizar el estado. No puedes reproducir.

Las redes de Petri resuelven ambos problemas porque son:

- **Inherentemente concurrentes.** Múltiples transiciones pueden dispararse simultáneamente. No necesitas pensar en pools de hilos o async/await — la concurrencia es una propiedad matemática de la red.
- **Formalmente analizables.** Puedes probar propiedades como alcanzabilidad (¿puede el sistema alcanzar el estado X?), vivacidad (¿se disparará eventualmente la transición T?) y acotamiento (¿crecerá la memoria sin límite?).
- **Visuales.** Una red de Petri es un diagrama. Puedes dibujarla en una pizarra. No-ingenieros pueden entenderla. Esto importa cuando estás depurando por qué tu agente hizo algo inesperado.
- **Con estado explícito.** El estado de todo el sistema es la distribución de tokens a través de los lugares. Puedes tomar un snapshot, serializarlo, restaurarlo, compararlo.

La extensión **Coloreada** añade tipos a los tokens. En lugar de puntos negros anónimos, los tokens llevan datos estructurados — un mensaje del usuario, un plan JSON, un resultado de clasificación, una aprobación humana. Los lugares se convierten en contenedores tipados. Las transiciones se vuelven condicionales según los tipos de token. Esto es lo que hace a las CPNs lo suficientemente poderosas como para modelar flujos de trabajo reales de agentes.

### La base académica

Este trabajo está fundamentado en el artículo ["Human-Artificial Interaction in the Age of Agentic AI: A System-Theoretical Approach"](https://arxiv.org/abs/2502.14000) (Borghoff, Bottoni, Pareschi — 2025), que formaliza el uso de Redes de Petri Coloreadas para modelar tanto sistemas multi-agente (MAS) como sistemas humano-IA profundamente integrados (arquitecturas "Centaurianas"). Las contribuciones clave de ese artículo que implementamos aquí:

- **Espacios de Comunicación** — particionar los lugares en capas de Superficie, Observación y Computación
- **Ejecución en modo dual** — modos MAS (autónomo) y Centauriano (co-disparo humano) en la misma red
- **Agentes de grupo** — gestión dinámica de equipos con protocolos de registro/entrega/desregistro
- **Teoría de sistemas vivientes** — fronteras claras, interacciones reguladas, bucles de retroalimentación adaptativos

---

## 2. Fundamentos Matemáticos

> *"Si quieres construir un barco, no reúnas gente para juntar madera y no les asignes tareas y trabajo, sino enséñales a anhelar la inmensidad infinita del mar." — Antoine de Saint-Exupery*

Antes de escribir una sola línea de código, necesitas entender las matemáticas. Esta sección está escrita para ingenieros, no para matemáticos — usaremos definiciones precisas pero explicaremos cada símbolo.

### 2.1 Red de Petri Clásica

Una red de Petri es una tupla **(P, T, F, M₀)** donde:

- **P** = un conjunto finito de *lugares* (dibujados como círculos)
- **T** = un conjunto finito de *transiciones* (dibujadas como rectángulos)
- **F** ⊆ (P × T) ∪ (T × P) = un conjunto de *arcos* que conectan lugares con transiciones y transiciones con lugares
- **M₀**: P → N = el *marcado inicial* — cuántos tokens tiene cada lugar al inicio

La red es **bipartita**: los arcos solo conectan lugares con transiciones o transiciones con lugares, nunca lugar-a-lugar o transición-a-transición.

**Regla de disparo:** Una transición *t* está *habilitada* (puede dispararse) cuando cada lugar de entrada tiene al menos un token. Cuando *t* se dispara:
1. Se remueve un token de cada lugar de entrada
2. Se añade un token a cada lugar de salida

Eso es todo. Todo lo demás — concurrencia, sincronización, exclusión mutua, productor-consumidor — emerge de esta regla simple aplicada a distintas topologías.

```
  Antes del disparo:         Después del disparo:

  [P1: ●●] → T1 → [P2: ]     [P1: ●] → T1 → [P2: ●]

  Token consumido de P1, depositado en P2
```

### 2.2 Red de Petri Coloreada (CPN)

Una CPN extiende la red clásica con tipos. Formalmente, una CPN es una tupla **(P, T, A, Σ, C, G, E, M₀)** donde:

- **Σ** = un conjunto finito de *conjuntos de colores* (tipos). Piénsalos como tu sistema de tipos: `STRING`, `JSON`, `ARTIFACT`, `HUMAN`, `ERROR`, etc.
- **C**: P → Σ = la *función de color* que asigna un tipo a cada lugar. El lugar P1 podría aceptar solo tokens `STRING`, P2 solo tokens `JSON`.
- **G** = *funciones guarda* sobre las transiciones. Una guarda es un predicado sobre los tokens de entrada. La transición solo se dispara si la guarda devuelve verdadero.
- **E** = *expresiones de arco* que determinan qué tokens fluyen por qué arcos.

En la práctica, esto significa:

```
Lugar "user_input" (Color: STRING, Espacio: Surface)
  contiene: Token{Color: STRING, Payload: "¿Qué clima hace?", Origin: human}

Lugar "classification" (Color: JSON, Espacio: Computation)
  contiene: Token{Color: JSON, Payload: {"intent": "weather_query"}, Origin: llm}
```

Una transición con una guarda podría verse así:

```
Transition "route_to_weather" {
  InputPlaces:  ["classification"]
  OutputPlaces: ["weather_agent_input"]
  Guard: func(tokens) -> tokens[0].Payload.intent == "weather_query"
}
```

### 2.3 Propiedades Clave que Obtienes Gratis

Una vez que tu agente está modelado como una CPN, puedes verificar formalmente:

| Propiedad | Pregunta que responde | Por qué importa para agentes |
|----------|-------------------|--------------------------|
| **Alcanzabilidad** | ¿Puede el sistema alcanzar el estado X? | "¿Puede mi agente alcanzar alguna vez un estado donde envía un correo sin aprobación?" |
| **Vivacidad** | ¿Se disparará eventualmente la transición T? | "¿Se desbloqueará alguna vez mi compuerta de aprobación HITL?" |
| **Acotamiento** | ¿Puede el lugar P acumular tokens sin límite? | "¿Crecerá mi cola de mensajes para siempre si el LLM es lento?" |
| **Ausencia de bloqueo (deadlock freedom)** | ¿Puede el sistema llegar a un estado donde nada pueda dispararse? | "¿Puede mi agente quedarse atascado sin progreso posible?" |
| **Equidad** | ¿Se disparará eventualmente cada transición habilitada? | "¿Dejará la ruta de reintento sin ejecutar al camino feliz?" |

En la mayoría de los frameworks de agentes, descubres los bloqueos en producción a las 3 AM. Con CPNs, los descubres en tiempo de compilación.

### 2.4 La Intuición del Grafo Bipartito

Piensa en una CPN como un **grafo dirigido bipartito**:

```
         ┌──────────┐        ┌──────────┐        ┌──────────┐
         │  Lugar    │───────▶│Transición│───────▶│  Lugar    │
         │  (datos)  │        │(cómputo) │        │  (datos)  │
         └──────────┘        └──────────┘        └──────────┘
              ○                    ■                    ○
           "buffer"             "acción"            "buffer"
```

- **Los lugares** son pasivos. Contienen datos. Son buffers, colas, registros.
- **Las transiciones** son activas. Transforman datos. Son funciones, llamadas a LLM, invocaciones de herramientas, compuertas de aprobación humana.
- **Los tokens** son los datos en sí. Fluyen a través de la red, llevando payloads, metadatos de origen e información de tipo.

---

## 3. De las Matemáticas al Código: Tipos Centrales

Así es como las primitivas matemáticas se mapean a código. Mostramos los tres lenguajes uno al lado del otro.

### 3.1 Token — La Unidad de Datos

Un token es la unidad fundamental de datos que fluye por la red. Lleva un payload tipado, su origen (quién lo creó) y su identidad espacial (a qué capa de comunicación pertenece).

**Go:**
```go
type Token struct {
    Color       ColorSet   // Clasificación de tipo: STRING, JSON, ARTIFACT, HUMAN, ERROR
    Payload     any        // Los datos reales. Los consumidores hacen type-assert.
    OriginID    string     // ID de la CPN que produjo este token
    OriginDepth int        // Profundidad del productor (0=raíz, 1=dominio, 2+=worker, -1=humano)
    OriginKind  NodeKind   // Tipo de transición que produjo esto (llm, tool, hitl...)
    Space       SpaceKind  // Capa de comunicación: surface, observation, computation
    SessionID   string     // Vincula a la sesión del usuario
    TraceID     string     // Trazado distribuido
    Timestamp   time.Time  // Tiempo de creación
}

// IsHumanOrigin devuelve true si este token fue producido por un humano.
// Usado por las funciones guarda del modo Centauriano.
func (t *Token) IsHumanOrigin() bool {
    return t.Color == ColorHuman || t.OriginKind == NodeKindHITL
}
```

**Rust:**
```rust
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::any::Any;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub enum ColorSet {
    String,
    Json,
    Artifact,
    Score,
    Event,
    Human,
    Error,
    Schema,
    Identity,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub enum SpaceKind {
    Surface,
    Observation,
    Computation,
}

#[derive(Debug, Clone)]
pub struct Token {
    pub color: ColorSet,
    pub payload: Box<dyn Any + Send + Sync>,
    pub origin_id: String,
    pub origin_depth: i32,
    pub origin_kind: NodeKind,
    pub space: SpaceKind,
    pub session_id: String,
    pub trace_id: String,
    pub timestamp: DateTime<Utc>,
}

impl Token {
    pub fn is_human_origin(&self) -> bool {
        self.color == ColorSet::Human || self.origin_kind == NodeKind::HITL
    }
}
```

**Elixir:**
```elixir
defmodule CPN.Token do
  @moduledoc """
  La unidad fundamental de datos en la CPN.
  Inmutable — creada una vez, nunca modificada en sitio.
  """

  @type color_set :: :string | :json | :artifact | :score | :event
                   | :human | :error | :schema | :identity

  @type space_kind :: :surface | :observation | :computation

  @type t :: %__MODULE__{
    color: color_set(),
    payload: any(),
    origin_id: String.t(),
    origin_depth: integer(),
    origin_kind: CPN.NodeKind.t(),
    space: space_kind(),
    session_id: String.t(),
    trace_id: String.t(),
    timestamp: DateTime.t()
  }

  defstruct [
    :color, :payload, :origin_id, :origin_depth, :origin_kind,
    :space, :session_id, :trace_id, :timestamp
  ]

  @spec human_origin?(t()) :: boolean()
  def human_origin?(%__MODULE__{color: :human}), do: true
  def human_origin?(%__MODULE__{origin_kind: :hitl}), do: true
  def human_origin?(_), do: false
end
```

### 3.2 Lugar — El Buffer Tipado

Un lugar es un contenedor tipado y thread-safe para tokens. Impone restricciones de color (seguridad de tipos) y restricciones de espacio (aislamiento de capas).

**Go:**
```go
type Place struct {
    ID     string
    Color  ColorSet    // Solo tokens de este color pueden depositarse
    Space  SpaceKind   // Capa de comunicación a la que pertenece este lugar
    Tokens []*Token    // El buffer de tokens (protegido por mutex)
    mu     sync.Mutex
}

func (p *Place) Deposit(t *Token) error {
    // Imponer aislamiento de espacio: tokens de surface no pueden saltar a computation
    if t.Space == SpaceSurface && p.Space == SpaceComputation {
        return ErrSpaceViolation
    }
    // Imponer seguridad de tipos
    if t.Color != p.Color {
        return ErrColorMismatch
    }
    if t.Space != p.Space {
        return ErrSpaceMismatch
    }
    p.mu.Lock()
    defer p.mu.Unlock()
    p.Tokens = append(p.Tokens, t)
    return nil
}

func (p *Place) Consume() (*Token, error) {
    p.mu.Lock()
    defer p.mu.Unlock()
    if len(p.Tokens) == 0 {
        return nil, ErrEmptyPlace
    }
    t := p.Tokens[0]
    p.Tokens = p.Tokens[1:]
    return t, nil
}
```

**Rust:**
```rust
use std::sync::Mutex;
use std::collections::VecDeque;

pub struct Place {
    pub id: String,
    pub color: ColorSet,
    pub space: SpaceKind,
    tokens: Mutex<VecDeque<Token>>,
}

impl Place {
    pub fn new(id: String, color: ColorSet, space: SpaceKind) -> Self {
        Place {
            id,
            color,
            space,
            tokens: Mutex::new(VecDeque::new()),
        }
    }

    pub fn deposit(&self, token: Token) -> Result<(), CPNError> {
        if token.space == SpaceKind::Surface && self.space == SpaceKind::Computation {
            return Err(CPNError::SpaceViolation);
        }
        if token.color != self.color {
            return Err(CPNError::ColorMismatch);
        }
        let mut tokens = self.tokens.lock().unwrap();
        tokens.push_back(token);
        Ok(())
    }

    pub fn consume(&self) -> Result<Token, CPNError> {
        let mut tokens = self.tokens.lock().unwrap();
        tokens.pop_front().ok_or(CPNError::EmptyPlace)
    }

    pub fn peek(&self) -> Vec<Token> {
        let tokens = self.tokens.lock().unwrap();
        tokens.iter().cloned().collect()
    }

    pub fn len(&self) -> usize {
        self.tokens.lock().unwrap().len()
    }
}
```

**Elixir:**
```elixir
defmodule CPN.Place do
  @moduledoc """
  Un buffer tipado y concurrente para tokens.
  Implementado como un GenServer para acceso thread-safe vía la BEAM.
  """
  use GenServer

  defstruct [:id, :color, :space, tokens: :queue.new()]

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: via(opts[:id]))
  end

  def deposit(place_id, %CPN.Token{} = token) do
    GenServer.call(via(place_id), {:deposit, token})
  end

  def consume(place_id) do
    GenServer.call(via(place_id), :consume)
  end

  # --- Callbacks ---

  @impl true
  def init(opts) do
    {:ok, %__MODULE__{
      id: opts[:id],
      color: opts[:color],
      space: opts[:space]
    }}
  end

  @impl true
  def handle_call({:deposit, token}, _from, state) do
    with :ok <- validate_space(token.space, state.space),
         :ok <- validate_color(token.color, state.color) do
      {:reply, :ok, %{state | tokens: :queue.in(token, state.tokens)}}
    else
      {:error, _} = err -> {:reply, err, state}
    end
  end

  @impl true
  def handle_call(:consume, _from, state) do
    case :queue.out(state.tokens) do
      {{:value, token}, rest} -> {:reply, {:ok, token}, %{state | tokens: rest}}
      {:empty, _} -> {:reply, {:error, :empty_place}, state}
    end
  end

  defp validate_space(:surface, :computation), do: {:error, :space_violation}
  defp validate_space(s, s), do: :ok
  defp validate_space(_, _), do: {:error, :space_mismatch}

  defp validate_color(c, c), do: :ok
  defp validate_color(_, _), do: {:error, :color_mismatch}

  defp via(id), do: {:via, Registry, {CPN.Registry, id}}
end
```

### 3.3 Transición — La Unidad de Cómputo

Una transición es una acción condicional que se dispara cuando sus lugares de entrada tienen tokens y su guarda se satisface.

**Go:**
```go
type Transition struct {
    ID           string
    Kind         NodeKind   // tool, llm, validate, subnet, observer, hitl
    InputPlaces  []string   // IDs de los lugares de entrada
    OutputPlaces []string   // IDs de los lugares de salida
    ErrorPlace   string     // Fallback para tokens de error
    Guard        func(tokens []*Token) bool  // Condición opcional de disparo
    Retry        *RetryPolicy
    // Campos específicos por Kind (LLMConfig, SubNet, HITLConfig, etc.)
}

func (t *Transition) CanFire(places map[string]*Place) bool {
    if len(t.InputPlaces) == 0 {
        return false
    }
    var tokens []*Token
    for _, pid := range t.InputPlaces {
        p, ok := places[pid]
        if !ok { return false }
        ts, ok := p.Peek()
        if !ok { return false }
        tokens = append(tokens, ts...)
    }
    if t.Guard != nil {
        return t.Guard(tokens)
    }
    return true
}
```

**Rust:**
```rust
pub struct Transition {
    pub id: String,
    pub kind: NodeKind,
    pub input_places: Vec<String>,
    pub output_places: Vec<String>,
    pub error_place: Option<String>,
    pub guard: Option<Box<dyn Fn(&[Token]) -> bool + Send + Sync>>,
    pub retry: Option<RetryPolicy>,
    // Configuración específica por Kind vía enum
    pub config: TransitionConfig,
}

pub enum TransitionConfig {
    Tool { executor: Box<dyn Fn(Token) -> Result<Token, CPNError> + Send + Sync> },
    LLM(LLMConfig),
    Validate(ValidateConfig),
    SubNet { factory: Box<dyn Fn() -> CPN + Send + Sync> },
    Observer { observed_cpn_id: Option<String> },
    HITL(HITLConfig),
}

impl Transition {
    pub fn can_fire(&self, places: &HashMap<String, Place>) -> bool {
        if self.input_places.is_empty() { return false; }

        let mut tokens = Vec::new();
        for pid in &self.input_places {
            match places.get(pid) {
                Some(place) => {
                    let peeked = place.peek();
                    if peeked.is_empty() { return false; }
                    tokens.extend(peeked);
                }
                None => return false,
            }
        }

        match &self.guard {
            Some(guard) => guard(&tokens),
            None => true,
        }
    }
}
```

**Elixir:**
```elixir
defmodule CPN.Transition do
  @type node_kind :: :tool | :llm | :validate | :subnet | :observer | :hitl

  @type t :: %__MODULE__{
    id: String.t(),
    kind: node_kind(),
    input_places: [String.t()],
    output_places: [String.t()],
    error_place: String.t() | nil,
    guard: (list(CPN.Token.t()) -> boolean()) | nil,
    config: map()
  }

  defstruct [:id, :kind, :input_places, :output_places, :error_place, :guard, :config]

  @spec can_fire?(t(), map()) :: boolean()
  def can_fire?(%__MODULE__{input_places: []}, _places), do: false
  def can_fire?(%__MODULE__{} = t, places) do
    tokens =
      Enum.flat_map(t.input_places, fn pid ->
        case Map.get(places, pid) do
          nil -> throw(:missing_place)
          place ->
            case CPN.Place.peek(place.id) do
              {:ok, tokens} when tokens != [] -> tokens
              _ -> throw(:empty_place)
            end
        end
      end)

    case t.guard do
      nil -> true
      guard_fn -> guard_fn.(tokens)
    end
  catch
    :missing_place -> false
    :empty_place -> false
  end
end
```

### 3.4 La CPN — El Tipo Universal de Agente

Esta es la idea crítica: **una CPN es el único tipo de agente que necesitas.** Una CPN en profundidad 0 es un coordinador raíz. En profundidad 1, un especialista de dominio. En profundidad 2+, un worker. El comportamiento emerge de la topología, no de una jerarquía de clases.

**Go:**
```go
type CPN struct {
    ID          string
    Role        string             // "coordinator", "researcher", "coder", etc.
    Depth       int                // 0=raíz, 1=dominio, 2+=worker
    Mode        Mode               // "mas" (autónomo) o "centaurian" (co-disparo humano)
    State       State              // idle, running, waiting, completed, failed
    Places      map[string]*Place
    Transitions map[string]*Transition
    SessionID   string
    LLMClient   LLMClient          // Interfaz puerto para llamadas al LLM
    History     []*Message         // Contexto de conversación
    Group       *GroupAgent        // Administra el equipo de sub-CPN
    EventSink   func(*Event)       // Callback de eventos
}
```

---

## 4. Las Cinco Primitivas

Toda operación de CPN se reduce a cinco operaciones primitivas. Domina éstas y comprenderás todo el motor.

| # | Primitiva | Descripción | Seguridad de Hilos |
|---|-----------|-------------|---------------|
| 1 | **Deposit** | Añadir un token a un lugar | Mutex en el lugar |
| 2 | **Consume** | Remover el token más antiguo de un lugar (FIFO) | Mutex en el lugar |
| 3 | **Peek** | Leer tokens sin removerlos (snapshot) | Peek de solo lectura |
| 4 | **CanFire** | Verificar si las entradas de una transición están satisfechas + la guarda pasa | Peek de solo lectura |
| 5 | **Fire** | Consumir entradas, ejecutar cómputo, depositar salidas | Consumir en goroutine principal, computar en paralelo |

La idea crítica de ingeniería es el patrón **consumir-antes-de-lanzar**:

```
Hilo principal:
  1. Recolectar todas las transiciones disparables
  2. Para cada una: re-verificar CanFire (puede haber cambiado)
  3. CONSUMIR tokens en el hilo principal
  4. Lanzar goroutines de fire con los tokens consumidos (por valor, no por referencia)
  5. Esperar a que todas las goroutines completen

Esto previene que dos transiciones consuman el mismo token.
```

---

## 5. Espacios de Comunicación

Los espacios de comunicación son las capas conceptuales que particionan los lugares de una CPN en zonas distintas con roles diferentes. Esto viene directamente del framework del artículo académico (Sección 4) y se mapea precisamente al patrón MVC:

```
┌─────────────────────────────────────────────────┐
│               ESPACIO SURFACE                    │
│  Cara al usuario: prompts, respuestas, UI        │
│  Tipos de token: STRING, ARTIFACT, HUMAN         │
│  Analogía: capa View                             │
├─────────────────────────────────────────────────┤
│             ESPACIO OBSERVATION                  │
│  Eventos de sub-CPNs, enrutamiento, coordinación │
│  Tipos de token: EVENT                           │
│  Analogía: capa Controller                       │
├─────────────────────────────────────────────────┤
│             ESPACIO COMPUTATION                  │
│  Procesamiento interno: clasificación, planning  │
│  Tipos de token: JSON, SCORE, SCHEMA             │
│  Analogía: capa Model                            │
└─────────────────────────────────────────────────┘
```

### La Regla de Aislamiento de Espacios

**Un token de surface no puede saltar directamente a un lugar de computation.** Esto se impone en el momento del depósito y se valida al construir la topología. La razón: la entrada cruda del usuario debe pasar por al menos una transición (clasificación, parseo, sanitización) antes de entrar a la capa de computación.

La **única excepción** son las transiciones HITL. Una transición HITL es el puente autorizado entre surface y computation — es cómo los tokens de aprobación humana entran a la capa de computación. Esto es intencional: el juicio humano es la única cosa que debería poder esquivar el pipeline normal de procesamiento.

```
PERMITIDO:
  Surface → [Transición] → Observation → [Transición] → Computation
  Surface → [Transición HITL] → Computation   (puente autorizado)

PROHIBIDO:
  Surface → [Transición regular] → Computation  (¡violación de espacio!)
```

---

## 6. El Bucle Ejecutor

El ejecutor es el corazón del motor. Es un único bucle que corre hasta que la CPN se completa, se bloquea (deadlock), expira o falla. Aquí está el algoritmo:

```
func (c *CPN) Run(ctx context.Context) error {
    // 1. Validar topología antes de empezar
    if err := Validate(c.Places, c.Transitions); err != nil {
        return err
    }
    c.State = Running

    for {
        // 2. Verificar cancelación del contexto (timeout, cancel del padre)
        if ctx.Err() != nil {
            return ErrTimeout
        }

        // 3. Drenar eventos de observer desde los buses de sub-CPN
        drainObservers(ctx, c)

        // 4. Recolectar transiciones disparables (consciente del modo)
        firable := c.collectFirable()

        // 5. ¿Nada disparable? Verificar completación o bloqueo (deadlock)
        if len(firable) == 0 {
            if c.IsComplete() {
                c.State = Completed
                return nil
            }
            if c.activeChildren > 0 {
                // Esperar a los hijos — pueden depositar tokens
                c.childWg.Wait()
                continue  // Re-evaluar
            }
            return ErrDeadlock
        }

        // 6. Ordenar para determinismo, consumir tokens en el hilo principal
        sort.Slice(firable, func(i, j int) bool {
            return firable[i].ID < firable[j].ID
        })

        var firings []firing
        for _, t := range firable {
            if !c.effectiveCanFire(t) { continue }  // Re-verificar después de consumos previos
            consumed := consumeAll(t.InputPlaces, c.Places)
            firings = append(firings, firing{t, consumed})
        }

        // 7. Lanzar goroutines de fire en paralelo
        var wg sync.WaitGroup
        errCh := make(chan fireResult, len(firings))
        for _, f := range firings {
            wg.Add(1)
            go func(t *Transition, consumed []Token) {
                defer wg.Done()
                _, _, err := dispatch(ctx, t, c, consumed)
                if err != nil {
                    errCh <- fireResult{t.ID, err}
                }
            }(f.transition, f.consumed)
        }
        wg.Wait()
        close(errCh)

        // 8. Procesar errores: enrutar a ErrorPlace o fallar la CPN
        for fr := range errCh {
            t := c.Transitions[fr.transitionID]
            if t.ErrorPlace != "" {
                // Enrutar token de error a ErrorPlace para recuperación
                depositErrorToken(c, t, fr.err)
                continue
            }
            return fr.err  // Sin recuperación — la CPN falla
        }

        // 9. Verificar transiciones de modo (MAS ↔ Centauriano)
        c.checkModeSwitch()
    }
}
```

### Detección de Completación

Una CPN está completa cuando **todos los lugares terminales tienen al menos un token.** Un lugar terminal es uno que no es entrada de ninguna transición — es un "sumidero" en el grafo.

```go
func (c *CPN) IsComplete() bool {
    terminals := c.TerminalPlaces()
    if len(terminals) == 0 { return false }
    for _, p := range terminals {
        if p.Len() == 0 { return false }
    }
    return true
}

func (c *CPN) TerminalPlaces() []*Place {
    inputRefs := make(map[string]bool)
    for _, t := range c.Transitions {
        for _, pid := range t.InputPlaces {
            inputRefs[pid] = true
        }
    }
    var terminals []*Place
    for id, p := range c.Places {
        if !inputRefs[id] {
            terminals = append(terminals, p)
        }
    }
    return terminals
}
```

---

## 7. Tipos de Transición: Los Seis Tipos de Nodo

Toda transición tiene un `Kind` que determina su comportamiento de disparo. Hay exactamente seis tipos:

### 7.1 Tool (`tool`)

Llama a una función externa o API. Determinista, sin LLM involucrado.

```go
// Una transición tool tiene una función Executor
t := &Transition{
    ID:   "t-fetch-weather",
    Kind: NodeKindTool,
    InputPlaces:  []string{"weather-query"},
    OutputPlaces: []string{"weather-result"},
    Executor: func(ctx context.Context, in Token) (Token, error) {
        query := in.Payload.(string)
        result, err := weatherAPI.Fetch(ctx, query)
        if err != nil { return Token{}, err }
        return Token{Color: ColorJSON, Payload: result}, nil
    },
}
```

### 7.2 LLM (`llm`)

Llama a un modelo de lenguaje. Soporta streaming, uso de herramientas (bucle agéntico), límites de presupuesto y ensamblado de contexto.

```go
t := &Transition{
    ID:   "t-respond",
    Kind: NodeKindLLM,
    InputPlaces:  []string{"classified-input"},
    OutputPlaces: []string{"response-output"},
    SystemPrompt: "You are a helpful assistant.",
    LLMConfig: &LLMConfig{
        Model:        "anthropic/claude-sonnet-4-20250514",
        MaxTokens:    4096,
        Temperature:  0.7,
        StreamOutput: true,   // Streaming en tiempo real al usuario
        Budget:       0.10,   // Máximo $0.10 por llamada
    },
    LLMTools: []string{"t-fetch-weather", "t-search-web"},  // Herramientas que el LLM puede invocar
}
```

La transición LLM implementa un **bucle agéntico completo de tool-call**:
1. Llamar al LLM con herramientas disponibles
2. Si el LLM pide una herramienta → ejecutarla → retroalimentar el resultado al LLM
3. Repetir hasta que el LLM devuelva contenido final (limitado a 10 iteraciones)

### 7.3 Validate (`validate`)

Validación de esquema con autocorrección. Si el payload no coincide con el esquema esperado, un LLM puede intentar arreglarlo.

```go
t := &Transition{
    ID:   "t-validate-plan",
    Kind: NodeKindValidate,
    InputPlaces:  []string{"raw-plan"},
    OutputPlaces: []string{"valid-plan"},
    ErrorPlace:   "plan-errors",
    ValidateConfig: &ValidateConfig{
        Schema:          &PlanSchema{},    // Struct de Go para validación round-trip
        MaxCorrections:  3,                // Hasta 3 intentos de corrección por LLM
        CorrectionLLMID: "t-fix-json",    // Qué transición LLM usar para correcciones
    },
}
```

### 7.4 SubNet (`subnet`)

Genera una CPN hija en una nueva goroutine. El padre continúa — no se bloquea. Cuando la hija completa, sus tokens de salida se depositan de vuelta en el padre.

```go
t := &Transition{
    ID:   "t-research",
    Kind: NodeKindSubNet,
    InputPlaces:  []string{"research-task"},
    OutputPlaces: []string{"research-result"},
    SubNetFactory: func() *CPN {
        return buildResearcherCPN()  // Devuelve una topología CPN fresca
    },
}
```

### 7.5 Observer (`observer`)

Observa eventos de sub-CPNs. Deposita tokens de evento en lugares del espacio observation para procesamiento posterior.

```go
t := &Transition{
    ID:   "t-watch-research",
    Kind: NodeKindObserver,
    OutputPlaces:  []string{"research-events"},
    ObservedCPNID: "",  // Vacío = observar a todos los hijos
    EventFilter: func(e Event) bool {
        return e.Type == EventSubNetCompleted || e.Type == EventSubNetFailed
    },
}
```

### 7.6 HITL (`hitl`)

Humano-en-el-ciclo. Bloquea la CPN hasta que un humano provee entrada (aprobar, rechazar o revisar).

```go
hitlCh := make(chan Token, 1)

t := &Transition{
    ID:   "t-approve-plan",
    Kind: NodeKindHITL,
    InputPlaces:  []string{"proposed-plan"},
    OutputPlaces: []string{"approved-plan"},
    HITLConfig: &HITLConfig{
        Channel:      hitlCh,
        Prompt:       "Please review this plan and approve, reject, or revise.",
        RevisionLoop: true,          // Soporte de revisión multi-ronda
        MaxRevisions: 5,
        CorrectionLLMID: "t-revise", // LLM para aplicar feedback de revisión
    },
}
```

---

## 8. El Sistema de Modo Dual: MAS y Centauriano

Este es uno de los aspectos más novedosos del motor. Una única CPN puede operar en dos modos:

### 8.1 Modo MAS (Sistema Multi-Agente)

En modo MAS, las transiciones se disparan autónomamente. El LLM clasifica, planifica y ejecuta sin intervención humana. Este es el default para la mayoría de interacciones.

```
Usuario: "¿Qué clima hace en Tokio?"
→ classify → route → fetch_weather_tool → respond
  (Todo automático, sin humano)
```

### 8.2 Modo Centauriano

En modo Centauriano, las transiciones del espacio computation requieren TANTO un token de origen humano COMO un token de IA para dispararse. Ni el humano ni la IA pueden disparar computación por sí solos.

```go
// La guarda Centauriana — inyectada automáticamente en modo Centauriano
func centaurianGuard(tokens []*Token) bool {
    var hasHuman, hasAI bool
    for _, tok := range tokens {
        if tok.IsHumanOrigin() {
            hasHuman = true
        } else {
            hasAI = true
        }
        if hasHuman && hasAI {
            return true
        }
    }
    return false
}
```

### 8.3 Cambio Automático de Modo

El modo cambia dinámicamente según el estado de los tokens:

- **MAS → Centauriano:** Cuando un token de origen humano entra en un lugar del espacio computation (vía aprobación HITL)
- **Centauriano → MAS:** Cuando ningún lugar de computation contiene tokens de origen humano Y ninguna transición HITL está disparable

Esto significa que el sistema opera autónomamente por defecto pero se desplaza automáticamente al modo colaborativo cuando un humano provee entrada que alcanza la capa de computación.

```
                    ┌─────────────┐
                    │  Modo MAS   │ (autónomo)
                    └──────┬──────┘
                           │ token humano entra al espacio computation
                           ▼
                    ┌──────────────┐
                    │  Centauriano │ (co-disparo requerido)
                    │     Modo     │
                    └──────┬───────┘
                           │ sin tokens humanos en computation + sin HITL disparable
                           ▼
                    ┌─────────────┐
                    │  Modo MAS   │ (de vuelta a autónomo)
                    └─────────────┘
```

---

## 9. Humano-en-el-Ciclo (HITL)

HITL se implementa como un tipo de transición, no como middleware especial. Esto significa que participa en las mismas reglas de disparo, condiciones de guarda y manejo de errores que cualquier otra transición.

### 9.1 HITL de un solo turno

```
1. Emitir EventHITLRequested con el prompt
2. Poner el estado de la CPN en "waiting"
3. Bloquear en el canal HITL (o timeout del contexto)
4. Recibir token de respuesta humana (ColorHuman)
5. Si action == "reject" → devolver ErrHITLRejected
6. Si action == "approve" → depositar token en los lugares de salida
7. Puente de espacio: ajustar el space del token para coincidir con el lugar de salida
8. Si la salida es espacio computation → disparar modo Centauriano
```

### 9.2 Bucle de Revisión Multi-Ronda

```
Ronda 0:
  IA produce borrador → HITL pide al humano que revise
  Humano: "revisar — hacerlo más conciso"

Ronda 1:
  LLM de revisión reescribe según el feedback → HITL pregunta de nuevo
  Humano: "revisar — añadir un ejemplo"

Ronda 2:
  LLM de revisión reescribe de nuevo → HITL pregunta de nuevo
  Humano: "aprobar"
  → Token aprobado depositado en la salida

(Limitado a MaxRevisions, default 5)
```

### 9.3 El Puente de Espacio

HITL es el **único** tipo de transición que puede cruzar la frontera surface-a-computation. Cuando un humano aprueba algo, el space del token se ajusta para coincidir con el lugar de salida. Esto es intencional: convierte al humano en el guardián explícito entre la capa cara-al-usuario y la capa interna de computación.

---

## 10. Sub-CPNs: Equipos Jerárquicos de Agentes

Una CPN puede generar CPNs hijas como sub-agentes. Esto crea una jerarquía:

```
Profundidad 0: Coordinador Raíz (orquesta el equipo)
├── Profundidad 1: CPN Researcher (reúne información)
│   ├── Profundidad 2: Worker de búsqueda web
│   └── Profundidad 2: Worker de lectura de documentos
├── Profundidad 1: CPN Analyst (procesa hallazgos)
└── Profundidad 1: CPN Writer (produce salida final)
```

### Ciclo de vida

1. La transición SubNet del padre se dispara
2. Se crea una CPN hija fresca (vía factory o clon)
3. La hija recibe `depth = parent.depth + 1`, hereda `sessionID`
4. Los tokens consumidos se inyectan en los lugares de origen de la hija
5. La hija corre en su propia goroutine — **el padre NO se bloquea**
6. Los eventos de la hija se envían al padre vía un canal con buffer (bus de eventos)
7. Cuando la hija completa, los tokens de sus lugares terminales se depositan en los lugares de salida del padre
8. Se añade un resumen comprimido al historial de conversación del padre

### Ejecución No Bloqueante

Esto es crítico: el padre continúa su bucle ejecutor mientras los hijos corren. Esto significa:

- Múltiples hijos pueden correr concurrentemente
- El padre puede disparar otras transiciones mientras espera
- Las transiciones observer pueden monitorear el progreso de los hijos en tiempo real
- El padre solo se bloquea cuando no tiene transiciones disparables Y aún hay hijos activos

```go
// El padre rastrea hijos activos con contador atómico + WaitGroup
parent.activeChildren.Add(1)
parent.childWg.Go(func() {
    defer parent.activeChildren.Add(-1)
    defer close(bus)  // Cerrar el bus de eventos cuando el hijo termine

    err := child.Run(ctx)
    if err != nil {
        // Enrutar error al ErrorPlace del padre o fallar al padre
    } else {
        // Depositar tokens de salida del hijo en los lugares de salida del padre
    }
})
```

---

## 11. Agentes de Grupo y Observación de Eventos

### Protocolo de Agente de Grupo

Del artículo académico (Sección 4.2), un agente de grupo gestiona un equipo de sub-CPNs:

```
register(agent)   — Añadir al conjunto activo o no-activo según estado
deliver(event)    — Fan-out del evento a todos los miembros activos (no bloqueante)
deregister(agent) — Remover del conjunto activo
switchCMP(agent)  — Mover entre activo/no-activo según cambio de estado
```

**Implementación en Go:**
```go
type GroupAgent struct {
    ID        string
    Topic     string
    active    []*CPN
    nonActive []*CPN
    mu        sync.RWMutex
}

func (g *GroupAgent) Deliver(e *Event) {
    g.mu.RLock()
    defer g.mu.RUnlock()
    for _, c := range g.active {
        if c.EventEmitter == nil { continue }
        select {
        case c.EventEmitter <- *e:
        default: // No bloqueante: saltar si el buffer está lleno
        }
    }
}
```

### Patrón Observer

Las transiciones observer drenan eventos de los buses de eventos hijos y los depositan como tokens en lugares del espacio observation:

```
Fase 1: Drenado no-bloqueante de TODOS los eventos de TODOS los buses hijos
Fase 2: Para cada evento × cada observer:
  - Verificar filtro ObservedCPNID
  - Verificar predicado EventFilter
  - Crear token ColorEvent
  - Depositar en los lugares de salida del observer
```

Esto corre en la goroutine principal, nunca se bloquea, nunca genera goroutines.

---

## 12. Memoria y Ensamblado de Contexto

### Estrategia de Memoria en Tres Niveles

Cuando una transición LLM se dispara, su ventana de contexto se ensambla en tres niveles:

```
┌────────────────────────────────────────────┐
│ T1: System Prompt (estático, siempre        │
│     incluido)                               │
├────────────────────────────────────────────┤
│ T2: Resúmenes de Observer (resultados       │
│     comprimidos de sub-CPN, siempre         │
│     incluidos)                              │
├────────────────────────────────────────────┤
│ T3: Ventana deslizante (últimos N turnos de │
│     conversación, user + assistant)         │
└────────────────────────────────────────────┘
```

- **T1** es el system prompt de la transición — estático, determinista
- **T2** lleva resúmenes comprimidos de las sub-CPNs. Cuando una hija completa, su salida se comprime en un mensaje RoleObserver y se añade al historial del padre.
- **T3** es una ventana deslizante de conversación cruda (default: últimos 10 turnos = 20 mensajes)

Esto te da:
- **Contexto acotado** — nunca excedes la ventana de contexto del LLM
- **Conciencia de sub-CPN** — el LLM padre sabe qué produjeron sus hijos
- **Sesgo de recencia** — los mensajes recientes siempre se incluyen; los viejos salen deslizándose

```go
func BuildContext(systemPrompt string, history []*Message, windowSize int) ContextWindow {
    // T2: Todos los mensajes de observer (nunca son desalojados)
    var observers []*LLMMessage
    for _, m := range history {
        if m.Role == RoleObserver {
            observers = append(observers, &LLMMessage{
                Role:    "assistant",
                Content: fmt.Sprintf("[Summary from %s]: %s", m.CPNRole, m.Content),
            })
        }
    }

    // T3: Ventana deslizante de mensajes user/assistant
    raw := filterUserAndAssistant(history)
    limit := windowSize * 2
    if len(raw) > limit {
        raw = raw[len(raw)-limit:]
    }

    messages := append(observers, toMessages(raw)...)
    return ContextWindow{SystemPrompt: systemPrompt, Messages: messages}
}
```

---

## 13. Reintento, Circuit Breakers y Resiliencia

### Política de Reintento

Cada transición puede tener una política de reintento con backoff exponencial:

```go
type RetryPolicy struct {
    MaxAttempts int            // Intentos totales (default: 1 = sin reintento)
    InitialWait time.Duration  // Espera antes del primer reintento
    MaxWait     time.Duration  // Tope del crecimiento exponencial
    Multiplier  float64        // Multiplicador de backoff (default: 2.0)
    RetryOn     func(err error, attempt int) bool  // Qué errores reintentar
}

// Política estándar: 3 intentos, 500ms → 1s → 2s, saltar errores de contexto
func DefaultRetryPolicy() *RetryPolicy {
    return &RetryPolicy{
        MaxAttempts: 3,
        InitialWait: 500 * time.Millisecond,
        MaxWait:     30 * time.Second,
        Multiplier:  2.0,
        RetryOn: func(err error, _ int) bool {
            return !errors.Is(err, context.Canceled) &&
                   !errors.Is(err, context.DeadlineExceeded)
        },
    }
}
```

### Circuit Breaker

Un circuit breaker rastrea fallas a través de todos los disparos de una transición:

```
Máquina de estados:
  CLOSED → (fallas ≥ umbral) → OPEN
  OPEN   → (tiempo transcurrido > duración) → HALF-OPEN
  HALF-OPEN → (éxito) → CLOSED
  HALF-OPEN → (falla) → OPEN
```

Cuando el circuito está OPEN, `CanFire` devuelve false — la transición está temporalmente deshabilitada. Esto previene que un proveedor LLM que esté fallando consuma todo tu presupuesto en reintentos.

**Implementación en Rust:**
```rust
use std::time::{Duration, Instant};
use std::sync::Mutex;

pub struct CircuitBreaker {
    config: CircuitBreakerConfig,
    state: Mutex<CBState>,
}

struct CBState {
    failures: u32,
    tripped_at: Option<Instant>,
}

pub struct CircuitBreakerConfig {
    pub failure_threshold: u32,
    pub open_duration: Duration,
}

impl CircuitBreaker {
    pub fn allow(&self) -> bool {
        let mut state = self.state.lock().unwrap();
        match state.tripped_at {
            None => true,
            Some(tripped) => {
                if tripped.elapsed() > self.config.open_duration {
                    state.tripped_at = None; // half-open
                    true
                } else {
                    false // aún abierto
                }
            }
        }
    }

    pub fn record_failure(&self) {
        let mut state = self.state.lock().unwrap();
        state.failures += 1;
        if state.failures >= self.config.failure_threshold {
            state.tripped_at = Some(Instant::now());
        }
    }

    pub fn record_success(&self) {
        let mut state = self.state.lock().unwrap();
        state.failures = 0;
        state.tripped_at = None;
    }
}
```

---

## 14. Validación y Autocorrección

La transición validate implementa un patrón crítico para agentes en producción: **salida estructurada con reparación automática.**

```
1. Consumir token del lugar de entrada
2. Validar payload contra el esquema (round-trip JSON o función personalizada)
3. Si es válido → aplicar transformación OnSuccess → depositar en salida
4. Si es inválido + hay correcciones disponibles:
   a. Llamar al LLM de corrección con mensaje de error y payload inválido
   b. Re-validar la salida corregida
   c. Repetir hasta MaxCorrections (tope de 10)
5. Si todas las correcciones se agotaron → enrutar a ErrorPlace
```

Esto cierra la brecha entre "los LLMs producen texto no estructurado" y "mi código downstream necesita datos tipados".

---

## 15. Sesiones: La Única Interfaz del Usuario

La sesión es la frontera entre el motor CPN y el mundo exterior. Los usuarios nunca ven topologías, transiciones ni tokens. Ven un stream.

```go
type Session struct {
    ID        string
    UserID    string
    Channel   ChannelType    // web, whatsapp, telegram
    Root      *CPN           // La CPN de profundidad 0 para esta sesión
    Stream    chan StreamChunk // Salida LLM en tiempo real al usuario
    hitlInject map[string]chan Token  // Canales de respuesta HITL
}

// El usuario envía un mensaje → se convierte en un token depositado en la CPN raíz
// La CPN raíz lo procesa → StreamChunks fluyen de vuelta al usuario vía SSE
```

### StreamChunk

```go
type StreamChunk struct {
    SessionID string
    CPNID     string  // Qué CPN produjo este chunk
    CPNRole   string  // "coordinator", "researcher", etc.
    Content   string  // El delta de texto
    Done      bool    // True para el chunk final
}
```

---

## 16. Validación de Topología

Antes de que cualquier CPN corra, su topología es validada. Esto atrapa bugs al arrancar, no a las 3 AM.

Verificaciones realizadas:

1. **Integridad de referencias de arco** — cada ID de lugar referenciado por una transición debe existir en el mapa de places
2. **Detección de violación de espacio** — ninguna transición puede cablear entradas surface directamente a salidas computation (excepto HITL)
3. **Configuración HITL** — las transiciones HITL deben tener HITLConfig no-nil con Channel no-nil
4. **Existencia de ErrorPlace** — si una transición referencia un ErrorPlace, debe existir

```go
func Validate(places map[string]*Place, transitions map[string]*Transition) error {
    var errs []error
    for _, t := range transitions {
        errs = append(errs, checkArcReferences(t, places)...)
        if t.Kind != NodeKindHITL {
            errs = append(errs, checkSpaceViolations(t, places)...)
        }
        errs = append(errs, checkHITLConfig(t)...)
    }
    if len(errs) > 0 {
        return &ValidationErrors{Errors: errs}
    }
    return nil
}
```

---

## 17. Seguimiento de Costos y Ranking de Flujos

### Métricas de Ejecución

Cada ejecución de CPN registra métricas: conteo de transiciones por tipo, tokens producidos, costo total, duración, éxito/falla.

### Ranking de Flujos

La fórmula de ranking implementa el principio: **el LLM es el último recurso.**

```
Score = LLMCallCount × LLMCallWeight + TotalCostUSD × CostWeight
```

Score más bajo = mejor flujo. Un flujo que usa 2 llamadas a herramientas y 1 llamada a LLM rankea mejor que un flujo que usa 5 llamadas a LLM. Esto incentiva topologías que usen herramientas y validación antes de recurrir a costosas llamadas LLM.

```go
func RankScore(rec *ExecutionRecord, w RankingWeights) float64 {
    return float64(rec.LLMCallCount)*w.LLMCallWeight + rec.TotalCostUSD*w.CostWeight
}
```

---

## 18. Arquitectura Hexagonal: Puertos y Adaptadores

El motor CPN sigue la arquitectura hexagonal estrictamente. El dominio (`cpn/`) define las interfaces puerto. La infraestructura las implementa.

### Interfaces Puerto

```go
// El dominio CPN define lo que necesita — no cómo se provee

type LLMClient interface {
    Complete(ctx context.Context, req *LLMRequest) (LLMResponse, error)
    CompleteStream(ctx context.Context, req *LLMRequest, onChunk func(string)) (LLMResponse, error)
    EstimateCost(req *LLMRequest) (float64, error)
}

type ChannelAdapter interface {
    Send(ctx context.Context, chunk StreamChunk) error
    Receive(ctx context.Context) (Message, error)
    Channel() ChannelType
}

type CostProvider interface {
    SessionCostUSD(sessionID string) float64
}
```

### Ejemplos de Adaptadores

```
Dominio (cpn/)            Adaptadores conducidos (infra/)   Adaptadores conductores (internal/driving/)
────────────────          ─────────────────────────────     ───────────────────────────────────────
LLMClient        ←──── OpenRouterClient                   Handlers HTTP API ────→ SessionService
ChannelAdapter   ←──── HTTPChannelAdapter                  SSE Broker             (Capa de Aplicación)
CostProvider     ←──── TokenLedger
```

El dominio NUNCA importa infraestructura. La infraestructura implementa las interfaces del dominio vía inversión de dependencias. Esto significa que puedes cambiar OpenRouter por Anthropic directo, o cambiar HTTP/SSE por WebSocket, sin tocar una sola línea del código de dominio.

---

## 19. Construyendo tu Primer Agente CPN (Paso a Paso)

Construyamos un agente simple que clasifica la intención del usuario y enruta al handler apropiado.

### Paso 1: Definir Lugares

```go
places := map[string]*Place{
    // Espacio surface — cara al usuario
    "p-input":    NewPlace("p-input",    ColorString,   SpaceSurface),
    "p-response": NewPlace("p-response", ColorArtifact, SpaceSurface),

    // Espacio computation — procesamiento interno
    "p-classified": NewPlace("p-classified", ColorJSON,     SpaceComputation),
    "p-greeting":   NewPlace("p-greeting",   ColorString,   SpaceComputation),
    "p-question":   NewPlace("p-question",   ColorString,   SpaceComputation),
}
```

### Paso 2: Definir Transiciones

```go
transitions := map[string]*Transition{
    // Classify: surface → computation (vía LLM)
    "t-classify": {
        ID: "t-classify", Kind: NodeKindLLM,
        InputPlaces:  []string{"p-input"},
        OutputPlaces: []string{"p-classified"},
        SystemPrompt: `Classify the user message. Return JSON: {"intent": "greeting"|"question"}`,
        LLMConfig: &LLMConfig{
            Model: "anthropic/claude-haiku-4-5-20251001", MaxTokens: 100,
            RequireJSON: true, SkipHistory: true,
        },
    },

    // Enrutar saludos
    "t-route-greeting": {
        ID: "t-route-greeting", Kind: NodeKindLLM,
        InputPlaces:  []string{"p-greeting"},
        OutputPlaces: []string{"p-response"},
        SystemPrompt: "Respond warmly to the greeting.",
        LLMConfig: &LLMConfig{
            Model: "anthropic/claude-haiku-4-5-20251001", MaxTokens: 200,
            StreamOutput: true,
        },
    },

    // Enrutar preguntas
    "t-route-question": {
        ID: "t-route-question", Kind: NodeKindLLM,
        InputPlaces:  []string{"p-question"},
        OutputPlaces: []string{"p-response"},
        SystemPrompt: "Answer the question helpfully.",
        LLMConfig: &LLMConfig{
            Model: "anthropic/claude-sonnet-4-20250514", MaxTokens: 2048,
            StreamOutput: true,
        },
    },

    // Validar clasificación y enrutar
    "t-validate-route": {
        ID: "t-validate-route", Kind: NodeKindValidate,
        InputPlaces:  []string{"p-classified"},
        OutputPlaces: []string{"p-greeting"},  // Ruta por defecto
        ValidateConfig: &ValidateConfig{
            ValidateFunc: func(payload any) error {
                // Lógica de enrutamiento personalizada aquí
                return nil
            },
        },
    },
}
```

### Paso 3: Crear y Ejecutar la CPN

```go
cpn := NewCPN("root", "classifier", 0, ModeMAS, sessionID, places, transitions)
cpn.LLMClient = myLLMClient

// Depositar el mensaje del usuario como token inicial
inputToken := &Token{
    Color: ColorString, Payload: "Hello, how are you?",
    Space: SpaceSurface, SessionID: sessionID,
    Timestamp: time.Now(),
}
places["p-input"].Deposit(inputToken)

// Correr la CPN
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

err := cpn.Run(ctx)
// El resultado está en p-response
```

### Paso 4: Añadir HITL (Opcional)

```go
// Añadir una compuerta de aprobación antes de la respuesta
"t-approve": {
    ID: "t-approve", Kind: NodeKindHITL,
    InputPlaces:  []string{"p-draft-response"},
    OutputPlaces: []string{"p-response"},
    HITLConfig: &HITLConfig{
        Channel:      hitlCh,
        Prompt:       "Review this response before sending.",
        RevisionLoop: true,
        MaxRevisions: 3,
    },
}
```

---

## 20. Ejemplos Completos Funcionales

### Ejemplo 1: CPN Mínima en Rust

```rust
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::Mutex;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Crear lugares
    let mut places = HashMap::new();
    places.insert("input".into(), Arc::new(Place::new(
        "input".into(), ColorSet::String, SpaceKind::Surface
    )));
    places.insert("output".into(), Arc::new(Place::new(
        "output".into(), ColorSet::Artifact, SpaceKind::Surface
    )));

    // Crear una transición tool
    let transition = Transition {
        id: "t-echo".into(),
        kind: NodeKind::Tool,
        input_places: vec!["input".into()],
        output_places: vec!["output".into()],
        error_place: None,
        guard: None,
        retry: None,
        config: TransitionConfig::Tool {
            executor: Box::new(|token: Token| {
                let payload = token.payload.downcast_ref::<String>()
                    .unwrap().clone();
                Ok(Token {
                    color: ColorSet::Artifact,
                    payload: Box::new(format!("Echo: {}", payload)),
                    origin_kind: NodeKind::Tool,
                    space: SpaceKind::Surface,
                    ..Default::default()
                })
            }),
        },
    };

    let mut transitions = HashMap::new();
    transitions.insert("t-echo".into(), transition);

    // Depositar token inicial
    places.get("input").unwrap().deposit(Token {
        color: ColorSet::String,
        payload: Box::new("Hello, CPN!".to_string()),
        space: SpaceKind::Surface,
        ..Default::default()
    })?;

    // Crear y correr CPN
    let mut cpn = CPN::new("root", "echo", 0, Mode::MAS, places, transitions);
    cpn.run().await?;

    // Leer resultado
    let result = places.get("output").unwrap().consume()?;
    println!("Result: {:?}", result.payload.downcast_ref::<String>());

    Ok(())
}
```

### Ejemplo 2: Equipo de Agentes Concurrentes en Elixir

La VM BEAM de Elixir es un ajuste natural para CPNs — cada lugar es un GenServer, cada disparo de transición es una Task, y el bucle ejecutor es una función recursiva en un GenServer.

```elixir
defmodule CPN.Engine do
  @moduledoc """
  El bucle ejecutor de la CPN, implementado como un GenServer.
  Cada instancia de CPN es un proceso — la BEAM nos da concurrencia gratis.
  """
  use GenServer

  defstruct [:id, :role, :depth, :mode, :state, :places, :transitions,
             :session_id, :llm_client]

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  def run(pid) do
    GenServer.call(pid, :run, :infinity)
  end

  @impl true
  def init(opts) do
    {:ok, struct!(__MODULE__, opts)}
  end

  @impl true
  def handle_call(:run, _from, state) do
    case validate_topology(state) do
      :ok ->
        result = executor_loop(%{state | state: :running})
        {:reply, result, state}
      {:error, _} = err ->
        {:reply, err, %{state | state: :failed}}
    end
  end

  defp executor_loop(%{state: :running} = cpn) do
    firable = collect_firable(cpn)

    case firable do
      [] ->
        if complete?(cpn), do: {:ok, :completed}, else: {:error, :deadlock}

      transitions ->
        # Consumir tokens en el proceso llamador (secuencial)
        firings = consume_and_prepare(transitions, cpn)

        # Lanzar cada disparo como una Task (concurrente, supervisada)
        tasks = Enum.map(firings, fn {transition, consumed} ->
          Task.async(fn -> dispatch(transition, cpn, consumed) end)
        end)

        # Esperar todas las tasks
        results = Task.await_many(tasks, 30_000)

        # Procesar resultados, depositar salidas
        case process_results(results, cpn) do
          {:ok, updated_cpn} -> executor_loop(updated_cpn)
          {:error, _} = err -> err
        end
    end
  end

  defp collect_firable(%{transitions: transitions, places: places}) do
    transitions
    |> Map.values()
    |> Enum.filter(&CPN.Transition.can_fire?(&1, places))
    |> Enum.sort_by(& &1.id)
  end

  defp complete?(%{places: places, transitions: transitions}) do
    terminal_ids = find_terminal_places(places, transitions)
    Enum.all?(terminal_ids, fn id ->
      case CPN.Place.len(id) do
        n when n > 0 -> true
        _ -> false
      end
    end)
  end
end
```

### Ejemplo 3: Flujo de Aprobación HITL en Go

```go
func buildApprovalCPN(sessionID string, llmClient LLMClient) (*CPN, chan Token) {
    hitlCh := make(chan Token, 1)

    places := map[string]*Place{
        "p-input":    NewPlace("p-input",    ColorString,   SpaceSurface),
        "p-draft":    NewPlace("p-draft",    ColorArtifact, SpaceComputation),
        "p-approved": NewPlace("p-approved", ColorArtifact, SpaceComputation),
        "p-output":   NewPlace("p-output",   ColorArtifact, SpaceSurface),
    }

    transitions := map[string]*Transition{
        "t-draft": {
            ID: "t-draft", Kind: NodeKindLLM,
            InputPlaces:  []string{"p-input"},
            OutputPlaces: []string{"p-draft"},
            SystemPrompt: "Draft a professional email based on the user's request.",
            LLMConfig: &LLMConfig{
                Model: "anthropic/claude-sonnet-4-20250514",
                MaxTokens: 2048, StreamOutput: true,
            },
        },
        "t-approve": {
            ID: "t-approve", Kind: NodeKindHITL,
            InputPlaces:  []string{"p-draft"},
            OutputPlaces: []string{"p-approved"},
            HITLConfig: &HITLConfig{
                Channel:         hitlCh,
                Prompt:          "Review the email draft. Approve, reject, or request revisions.",
                RevisionLoop:    true,
                MaxRevisions:    3,
                CorrectionLLMID: "t-revise",
            },
        },
        "t-revise": {
            ID: "t-revise", Kind: NodeKindLLM,
            InputPlaces:  []string{},  // Usado inline por el bucle de revisión HITL
            OutputPlaces: []string{},
            SystemPrompt: "Revise the email based on the feedback provided.",
            LLMConfig: &LLMConfig{
                Model: "anthropic/claude-sonnet-4-20250514", MaxTokens: 2048,
            },
        },
        "t-format": {
            ID: "t-format", Kind: NodeKindTool,
            InputPlaces:  []string{"p-approved"},
            OutputPlaces: []string{"p-output"},
            Executor: func(ctx context.Context, in Token) (Token, error) {
                return Token{
                    Color: ColorArtifact,
                    Payload: fmt.Sprintf("✉️ Final Email:\n\n%s", in.Payload),
                }, nil
            },
        },
    }

    cpn := NewCPN("email-agent", "drafter", 0, ModeMAS, sessionID, places, transitions)
    cpn.LLMClient = llmClient

    return cpn, hitlCh
}

// Uso:
// cpn, hitlCh := buildApprovalCPN(sessionID, llmClient)
// go cpn.Run(ctx)
// ... más tarde, cuando el usuario responda:
// hitlCh <- Token{Color: ColorHuman, Payload: HITLResponse{Action: HITLApprove}}
```

---

## 21. Ruta de Aprendizaje y Recursos

### Para Ingenieros Nuevos en Redes de Petri

1. **Empieza con las matemáticas** (Sección 2). Dibuja algunas redes en papel. Traza el flujo de tokens manualmente.
2. **Implementa las cinco primitivas** en tu lenguaje de elección. Haz que Deposit, Consume, Peek, CanFire y el bucle ejecutor funcionen con solo transiciones Tool.
3. **Añade transiciones LLM.** Conecta un cliente LLM. Haz un pipeline de clasificar-y-responder.
4. **Añade HITL.** Construye una compuerta de aprobación. Siente el poder de bloquear una CPN por input humano.
5. **Añade Sub-CPNs.** Construye un padre que genere dos hijos y fusione sus resultados.
6. **Añade Espacios de Comunicación.** Particiona tus lugares en surface/observation/computation. Añade validación de espacio.
7. **Añade el sistema de modo dual.** Implementa centaurianGuard y cambio automático de modo.

### Referencias Académicas Clave

- **Borghoff, Bottoni, Pareschi (2025)** — "Human-Artificial Interaction in the Age of Agentic AI: A System-Theoretical Approach" ([arXiv:2502.14000](https://arxiv.org/abs/2502.14000)). El artículo fundacional de esta arquitectura.
- **Jensen, K. (1995)** — "Coloured Petri Nets: Basic Concepts, Analysis Methods and Practical Use." La referencia definitiva de CPN.
- **Murata, T. (1989)** — "Petri nets: Properties, analysis and applications." Artículo clásico de survey.
- **Petri, C.A. (1962)** — "Kommunikation mit Automaten." La disertación original que lo inició todo.
- **Pareschi, R. (2024)** — "Beyond human and machine: An architecture and methodology guideline for centaurian design."
- **Simon, H.A. (1996)** — "The Sciences of the Artificial." El modelo tripartito (interfaz externa, mecanismo de codificación, procesamiento interno) que se mapea a nuestros tres espacios de comunicación.

### Notas de Concurrencia Específicas por Lenguaje

| Lenguaje | Mejor primitiva de concurrencia para CPN | Por qué |
|----------|-----------------------------------|-----|
| **Go** | Goroutines + channels + sync.Mutex | Concurrencia nativa. Cada transición se dispara en una goroutine. Los lugares usan mutexes. Los buses de eventos son channels. Cero dependencias. |
| **Rust** | Tasks de Tokio + Arc<Mutex<T>> | El sistema de ownership previene data races en tiempo de compilación. `Arc<Mutex<VecDeque<Token>>>` para los lugares. Tasks de Tokio para el disparo de transiciones. |
| **Elixir** | GenServer + Task.async | La VM BEAM es un ejecutor natural de CPN. Cada lugar es un proceso. Cada disparo es una task supervisada. OTP te da tolerancia a fallas gratis. |
| **Kotlin** | Coroutines + StateFlow | La concurrencia estructurada se mapea bien a la jerarquía CPN. Flows para streaming de eventos. |
| **Zig** | async/await + Thread pool | La gestión manual de memoria te da control absoluto sobre el ciclo de vida del token. Ideal para motores CPN embebidos. |

### Principios de Diseño

1. **Un tipo para gobernarlos a todos.** La CPN es el único tipo de agente. La identidad emerge de la topología y la profundidad. No crees clases `Agent`, `Tool`, `Coordinator` — todas son CPNs.

2. **Consumir antes de lanzar.** Siempre consume tokens en el hilo principal antes de lanzar goroutines de fire. Esto previene carreras de doble consumo.

3. **El aislamiento de espacios es una característica, no un bug.** La barrera surface→computation previene que la entrada cruda del usuario alcance el procesamiento interno sin transformación. HITL es el único puente autorizado.

4. **El LLM es el último recurso.** Prefiere herramientas, validación y enrutamiento sobre llamadas LLM. La fórmula de ranking de flujos penaliza los caminos pesados en LLM.

5. **Los eventos son append-only.** El log de eventos es la fuente de verdad de lo que pasó. Los eventos nunca se modifican ni se eliminan.

6. **La sesión es la única interfaz del usuario.** Los usuarios nunca ven CPNs, tokens ni transiciones. Ven un stream de chunks de texto y prompts HITL. Todo lo demás es invisible.

7. **Validar en construcción, no en runtime.** La validación de topología atrapa errores de arco, violaciones de espacio y transiciones HITL mal configuradas antes de que fluya el primer token.

8. **No bloqueante por defecto.** Las sub-CPNs corren asincrónicamente. El drenado de observer es no-bloqueante. El fan-out SSE usa envíos no-bloqueantes. Los clientes lentos se descartan, nunca bloquean el motor.

---

## Nota

Este documento describe un patrón, no un producto. La implementación exacta dependerá de tu lenguaje, tu proveedor de LLM, tu entorno de despliegue y tu caso de uso específico de agente. Los fundamentos matemáticos son universales. Los espacios de comunicación y el sistema de modo dual aplican independientemente del lenguaje de implementación. La arquitectura hexagonal asegura que puedas cambiar cualquier adaptador sin tocar el núcleo del dominio.

La mejor forma de usar este documento es elegir un lenguaje de los ejemplos de arriba, implementar la Sección 19 paso a paso, e iterar. Empieza con una única CPN que haga classify → respond. Añade complejidad solo cuando la versión simple funcione. La teoría estará ahí cuando la necesites.

Construye pequeño, verifica formalmente, escala horizontalmente.

---

*Este documento está basado en el motor de Red de Petri Coloreada que impulsa [liwaisi](https://github.com/liwaisi-tech), fundamentado en el framework académico de [Borghoff, Bottoni & Pareschi (2025)](https://arxiv.org/abs/2502.14000).*
