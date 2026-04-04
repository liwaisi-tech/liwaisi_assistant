import { useMemo, useState, useCallback } from 'react';
import {
  ReactFlow,
  Background,
  type Node,
  type Edge,
  useReactFlow,
  ReactFlowProvider,
} from '@xyflow/react';
import dagre from '@dagrejs/dagre';
import '@xyflow/react/dist/style.css';

import { PlaceNode } from './nodes/PlaceNode';
import { TransitionNode } from './nodes/TransitionNode';
import type { CPNTopology, TransitionTopology } from '../../types/flow';

const nodeTypes = {
  place: PlaceNode,
  transition: TransitionNode,
};

type Direction = 'LR' | 'TB';

interface TopologyGraphProps {
  topology: CPNTopology;
  firedTransitions?: Set<string>;
  onSelectTransition?: (t: TransitionTopology) => void;
}

function layoutGraph(nodes: Node[], edges: Edge[], direction: Direction): Node[] {
  const g = new dagre.graphlib.Graph();
  g.setGraph({
    rankdir: direction,
    nodesep: direction === 'LR' ? 60 : 80,
    ranksep: direction === 'LR' ? 140 : 100,
    marginx: 40,
    marginy: 40,
  });
  g.setDefaultEdgeLabel(() => ({}));

  for (const node of nodes) {
    const w = node.type === 'place' ? 76 : 180;
    const h = node.type === 'place' ? 76 : 80;
    g.setNode(node.id, { width: w, height: h });
  }
  for (const edge of edges) {
    g.setEdge(edge.source, edge.target);
  }

  dagre.layout(g);

  return nodes.map((node) => {
    const pos = g.node(node.id);
    const w = node.type === 'place' ? 76 : 180;
    const h = node.type === 'place' ? 76 : 80;
    return { ...node, position: { x: pos.x - w / 2, y: pos.y - h / 2 } };
  });
}

function GraphInner({ topology, firedTransitions, onSelectTransition }: TopologyGraphProps) {
  const [direction, setDirection] = useState<Direction>('LR');
  const { fitView } = useReactFlow();

  const toggleDirection = useCallback(() => {
    setDirection((d) => (d === 'LR' ? 'TB' : 'LR'));
    setTimeout(() => fitView({ padding: 0.15, duration: 300 }), 50);
  }, [fitView]);

  const { nodes, edges } = useMemo(() => {
    const rawNodes: Node[] = [];
    const rawEdges: Edge[] = [];

    let orderCounter = 1;
    const orderMap = new Map<string, number>();
    if (firedTransitions) {
      for (const tId of firedTransitions) {
        orderMap.set(tId, orderCounter++);
      }
    }

    // Places
    for (const [id, place] of Object.entries(topology.places)) {
      rawNodes.push({
        id,
        type: 'place',
        position: { x: 0, y: 0 },
        data: { place, direction },
      });
    }

    // Transitions + edges
    for (const [id, transition] of Object.entries(topology.transitions)) {
      const fired = firedTransitions ? firedTransitions.has(id) : undefined;
      rawNodes.push({
        id,
        type: 'transition',
        position: { x: 0, y: 0 },
        data: {
          transition,
          fired,
          executionOrder: orderMap.get(id),
          direction,
          onSelect: onSelectTransition,
        },
      });

      const edgeColor = fired === false ? '#1e293b' : fired ? '#475569' : '#334155';
      const edgeWidth = fired ? 2.5 : 1;

      for (const inputPlace of transition.inputPlaces) {
        rawEdges.push({
          id: `${inputPlace}->${id}`,
          source: inputPlace,
          target: id,
          animated: fired === true,
          style: { stroke: edgeColor, strokeWidth: edgeWidth },
          type: 'smoothstep',
        });
      }
      for (const outputPlace of transition.outputPlaces) {
        rawEdges.push({
          id: `${id}->${outputPlace}`,
          source: id,
          target: outputPlace,
          animated: fired === true,
          style: { stroke: edgeColor, strokeWidth: edgeWidth },
          type: 'smoothstep',
        });
      }
    }

    const laid = layoutGraph(rawNodes, rawEdges, direction);
    return { nodes: laid, edges: rawEdges };
  }, [topology, firedTransitions, direction, onSelectTransition]);

  return (
    <div className="w-full h-full relative" style={{ backgroundColor: '#080810' }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.15 }}
        minZoom={0.2}
        maxZoom={3}
        proOptions={{ hideAttribution: true }}
        nodesDraggable={false}
        defaultEdgeOptions={{ type: 'smoothstep' }}
      >
        <Background color="#141420" gap={24} size={1} />
      </ReactFlow>

      {/* Custom controls overlay */}
      <div
        className="absolute bottom-3 left-3 flex items-center gap-1.5 z-10"
      >
        {/* Direction toggle */}
        <button
          onClick={toggleDirection}
          className="flex items-center gap-1 px-2.5 py-1.5 rounded-lg text-[9px] font-semibold transition-all"
          style={{
            backgroundColor: 'var(--bg-surface)',
            border: '1px solid var(--border-dim)',
            color: 'var(--accent)',
            fontFamily: "'JetBrains Mono', monospace",
          }}
          title={`Switch to ${direction === 'LR' ? 'vertical' : 'horizontal'} layout`}
        >
          {direction === 'LR' ? '\u2194 Horizontal' : '\u2195 Vertical'}
        </button>

        {/* Fit view */}
        <button
          onClick={() => fitView({ padding: 0.15, duration: 300 })}
          className="px-2 py-1.5 rounded-lg text-[9px] transition-all"
          style={{
            backgroundColor: 'var(--bg-surface)',
            border: '1px solid var(--border-dim)',
            color: 'var(--text-muted)',
          }}
          title="Fit to view"
        >
          \u26F6
        </button>
      </div>
    </div>
  );
}

export function TopologyGraph(props: TopologyGraphProps) {
  return (
    <ReactFlowProvider>
      <GraphInner {...props} />
    </ReactFlowProvider>
  );
}
