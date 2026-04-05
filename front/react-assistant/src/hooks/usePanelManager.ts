import { useReducer, useCallback } from 'react';

type PanelContent = 'flows' | 'monitor' | null;

interface PanelState {
  panelContent: PanelContent;
  panelOpen: boolean;
  selectedFlowHash: string | null;
  selectedPanelFlowHash: string | null;
}

type PanelAction =
  | { type: 'OPEN_PANEL'; content: PanelContent }
  | { type: 'CLOSE_PANEL' }
  | { type: 'TOGGLE_PANEL'; content: PanelContent }
  | { type: 'SET_FLOW_HASH'; hash: string | null }
  | { type: 'SET_PANEL_FLOW_HASH'; hash: string | null }
  | { type: 'CLEAR_ALL' };

const initialState: PanelState = {
  panelContent: null,
  panelOpen: false,
  selectedFlowHash: null,
  selectedPanelFlowHash: null,
};

function panelReducer(state: PanelState, action: PanelAction): PanelState {
  switch (action.type) {
    case 'OPEN_PANEL':
      return { ...state, panelContent: action.content, panelOpen: true };

    case 'CLOSE_PANEL':
      return { ...state, panelContent: null, panelOpen: false, selectedPanelFlowHash: null };

    case 'TOGGLE_PANEL': {
      const isOpen = state.panelContent === action.content;
      return isOpen
        ? { ...state, panelContent: null, panelOpen: false, selectedPanelFlowHash: null }
        : { ...state, panelContent: action.content, panelOpen: true, selectedPanelFlowHash: null };
    }

    case 'SET_FLOW_HASH':
      return { ...state, selectedFlowHash: action.hash };

    case 'SET_PANEL_FLOW_HASH':
      return { ...state, selectedPanelFlowHash: action.hash };

    case 'CLEAR_ALL':
      return initialState;

    default:
      return state;
  }
}

export function usePanelManager() {
  const [state, dispatch] = useReducer(panelReducer, initialState);

  const openPanel = useCallback((content: PanelContent) => {
    dispatch({ type: 'OPEN_PANEL', content });
  }, []);

  const closePanel = useCallback(() => {
    dispatch({ type: 'CLOSE_PANEL' });
  }, []);

  const togglePanel = useCallback((content: PanelContent) => {
    dispatch({ type: 'TOGGLE_PANEL', content });
  }, []);

  const setFlowHash = useCallback((hash: string | null) => {
    dispatch({ type: 'SET_FLOW_HASH', hash });
  }, []);

  const setPanelFlowHash = useCallback((hash: string | null) => {
    dispatch({ type: 'SET_PANEL_FLOW_HASH', hash });
  }, []);

  const clearAll = useCallback(() => {
    dispatch({ type: 'CLEAR_ALL' });
  }, []);

  return {
    panelContent: state.panelContent,
    panelOpen: state.panelOpen,
    selectedFlowHash: state.selectedFlowHash,
    selectedPanelFlowHash: state.selectedPanelFlowHash,
    openPanel,
    closePanel,
    togglePanel,
    setFlowHash,
    setPanelFlowHash,
    clearAll,
  } as const;
}
