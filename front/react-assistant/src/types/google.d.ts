interface GoogleAccountsId {
  initialize: (config: {
    client_id: string;
    callback: (response: { credential: string }) => void;
    auto_select?: boolean;
  }) => void;
  prompt: (notification?: (n: { isNotDisplayed: () => boolean }) => void) => void;
  renderButton: (element: HTMLElement, config: {
    theme?: 'outline' | 'filled_blue' | 'filled_black';
    size?: 'large' | 'medium' | 'small';
    type?: 'standard' | 'icon';
    text?: string;
    width?: number;
  }) => void;
  disableAutoSelect: () => void;
  revoke: (email: string, callback: () => void) => void;
}

interface Window {
  google?: {
    accounts: {
      id: GoogleAccountsId;
    };
  };
}
