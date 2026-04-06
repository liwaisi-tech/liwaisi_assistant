export interface ConfigItem {
  key: string;
  value: string;
  is_secret: boolean;
  source: 'env' | 'db' | 'default';
  label: string;
  category: string;
  updated_at: string;
  updated_by: string;
}

export interface AdminConfigResponse {
  items: ConfigItem[];
  setup_required: boolean;
}

export interface PlatformStatusResponse {
  ready: boolean;
  missing_required: string[];
}
