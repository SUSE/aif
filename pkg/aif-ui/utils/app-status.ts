/**
 * App Status Calculation Utilities
 * Provides centralized status calculation and display logic for applications
 * Following standard patterns for consistent status management
 */

import { 
  APP_STATUS, 
  INSTALLATION_STATUS, 
  HEALTH_STATUS,
  CONNECTION_STATUS,
  NOTIFICATION_TYPE 
} from './constants';
import type { 
  AppStatus, 
  InstallationStatus, 
  HealthStatus, 
  ConnectionStatus,
  NotificationType 
} from './constants';

// === Status Display Configuration ===
export interface StatusDisplayConfig {
  color: string;
  bgColor: string;
  icon: string;
  label: string;
  description: string;
  priority: number; // Higher number = higher priority for aggregation
  actionable: boolean; // Whether user can take action
}

// === App Status Display Mapping ===
export const APP_STATUS_DISPLAY: Record<AppStatus, StatusDisplayConfig> = {
  [APP_STATUS.AVAILABLE]: {
    color: 'text-info',
    bgColor: 'bg-info',
    icon: 'icon-circle-notch',
    label: 'Available',
    description: 'Ready to install',
    priority: 1,
    actionable: true
  },
  [APP_STATUS.INSTALLING]: {
    color: 'text-warning',
    bgColor: 'bg-warning', 
    icon: 'icon-spinner',
    label: 'Installing',
    description: 'Installation in progress',
    priority: 4,
    actionable: false
  },
  [APP_STATUS.DEPLOYED]: {
    color: 'text-success',
    bgColor: 'bg-success',
    icon: 'icon-checkmark',
    label: 'Deployed',
    description: 'Successfully deployed and running',
    priority: 3,
    actionable: true
  },
  [APP_STATUS.UPGRADING]: {
    color: 'text-warning',
    bgColor: 'bg-warning',
    icon: 'icon-spinner',
    label: 'Upgrading', 
    description: 'Upgrade in progress',
    priority: 4,
    actionable: false
  },
  [APP_STATUS.UNINSTALLING]: {
    color: 'text-warning',
    bgColor: 'bg-warning',
    icon: 'icon-spinner',
    label: 'Uninstalling',
    description: 'Removal in progress',
    priority: 4,
    actionable: false
  },
  [APP_STATUS.FAILED]: {
    color: 'text-error',
    bgColor: 'bg-error',
    icon: 'icon-warning',
    label: 'Failed',
    description: 'Operation failed',
    priority: 5,
    actionable: true
  },
  [APP_STATUS.UNKNOWN]: {
    color: 'text-muted',
    bgColor: 'bg-muted',
    icon: 'icon-help',
    label: 'Unknown',
    description: 'Status cannot be determined',
    priority: 0,
    actionable: false
  }
};

// === Installation Status Display Mapping ===
export const INSTALLATION_STATUS_DISPLAY: Record<InstallationStatus, StatusDisplayConfig> = {
  [INSTALLATION_STATUS.PENDING]: {
    color: 'text-info',
    bgColor: 'bg-info',
    icon: 'icon-clock',
    label: 'Pending',
    description: 'Waiting to start',
    priority: 1,
    actionable: false
  },
  [INSTALLATION_STATUS.INSTALLING]: {
    color: 'text-warning',
    bgColor: 'bg-warning',
    icon: 'icon-spinner',
    label: 'Installing',
    description: 'Installation in progress',
    priority: 4,
    actionable: false
  },
  [INSTALLATION_STATUS.DEPLOYED]: {
    color: 'text-success',
    bgColor: 'bg-success',
    icon: 'icon-checkmark',
    label: 'Deployed',
    description: 'Successfully deployed',
    priority: 3,
    actionable: true
  },
  [INSTALLATION_STATUS.UPGRADING]: {
    color: 'text-warning',
    bgColor: 'bg-warning',
    icon: 'icon-spinner',
    label: 'Upgrading',
    description: 'Upgrade in progress',
    priority: 4,
    actionable: false
  },
  [INSTALLATION_STATUS.UNINSTALLING]: {
    color: 'text-warning',
    bgColor: 'bg-warning',
    icon: 'icon-spinner',
    label: 'Uninstalling',
    description: 'Removal in progress',
    priority: 4,
    actionable: false
  },
  [INSTALLATION_STATUS.FAILED]: {
    color: 'text-error',
    bgColor: 'bg-error',
    icon: 'icon-warning',
    label: 'Failed',
    description: 'Installation failed',
    priority: 5,
    actionable: true
  },
  [INSTALLATION_STATUS.SUPERSEDED]: {
    color: 'text-muted',
    bgColor: 'bg-muted',
    icon: 'icon-history',
    label: 'Superseded',
    description: 'Replaced by newer version',
    priority: 2,
    actionable: false
  },
  [INSTALLATION_STATUS.UNKNOWN]: {
    color: 'text-muted',
    bgColor: 'bg-muted',
    icon: 'icon-help',
    label: 'Unknown',
    description: 'Status cannot be determined',
    priority: 0,
    actionable: false
  }
};

// === Health Status Display Mapping ===
export const HEALTH_STATUS_DISPLAY: Record<HealthStatus, StatusDisplayConfig> = {
  [HEALTH_STATUS.HEALTHY]: {
    color: 'text-success',
    bgColor: 'bg-success',
    icon: 'icon-checkmark',
    label: 'Healthy',
    description: 'All components are functioning normally',
    priority: 4,
    actionable: false
  },
  [HEALTH_STATUS.DEGRADED]: {
    color: 'text-warning',
    bgColor: 'bg-warning',
    icon: 'icon-warning',
    label: 'Degraded',
    description: 'Some components have issues',
    priority: 3,
    actionable: true
  },
  [HEALTH_STATUS.UNHEALTHY]: {
    color: 'text-error',
    bgColor: 'bg-error',
    icon: 'icon-error',
    label: 'Unhealthy',
    description: 'Critical issues detected',
    priority: 2,
    actionable: true
  },
  [HEALTH_STATUS.UNKNOWN]: {
    color: 'text-muted',
    bgColor: 'bg-muted',
    icon: 'icon-help',
    label: 'Unknown',
    description: 'Health status unavailable',
    priority: 1,
    actionable: false
  }
};

// === Connection Status Display Mapping ===
export const CONNECTION_STATUS_DISPLAY: Record<ConnectionStatus, StatusDisplayConfig> = {
  [CONNECTION_STATUS.CONNECTED]: {
    color: 'text-success',
    bgColor: 'bg-success',
    icon: 'icon-link',
    label: 'Connected',
    description: 'Connection established',
    priority: 4,
    actionable: false
  },
  [CONNECTION_STATUS.CONNECTING]: {
    color: 'text-warning',
    bgColor: 'bg-warning',
    icon: 'icon-spinner',
    label: 'Connecting',
    description: 'Establishing connection',
    priority: 3,
    actionable: false
  },
  [CONNECTION_STATUS.DISCONNECTED]: {
    color: 'text-muted',
    bgColor: 'bg-muted',
    icon: 'icon-unlink',
    label: 'Disconnected',
    description: 'No connection',
    priority: 2,
    actionable: true
  },
  [CONNECTION_STATUS.ERROR]: {
    color: 'text-error',
    bgColor: 'bg-error',
    icon: 'icon-warning',
    label: 'Connection Error',
    description: 'Failed to connect',
    priority: 1,
    actionable: true
  }
};

// === Status Calculation Functions ===

/**
 * Calculate aggregated app status from multiple installation statuses
 */
export function calculateAppStatus(installationStatuses: InstallationStatus[]): AppStatus {
  if (!installationStatuses || installationStatuses.length === 0) {
    return APP_STATUS.AVAILABLE;
  }
  
  // Sort by priority (highest first)
  const prioritizedStatuses = installationStatuses
    .map(status => ({ status, priority: INSTALLATION_STATUS_DISPLAY[status]?.priority || 0 }))
    .sort((a, b) => b.priority - a.priority);
  
  const highestPriorityStatus = prioritizedStatuses[0]?.status;
  
  // Map installation status to app status
  switch (highestPriorityStatus) {
    case INSTALLATION_STATUS.INSTALLING:
      return APP_STATUS.INSTALLING;
    case INSTALLATION_STATUS.DEPLOYED:
      return APP_STATUS.DEPLOYED;
    case INSTALLATION_STATUS.UPGRADING:
      return APP_STATUS.UPGRADING;
    case INSTALLATION_STATUS.UNINSTALLING:
      return APP_STATUS.UNINSTALLING;
    case INSTALLATION_STATUS.FAILED:
      return APP_STATUS.FAILED;
    case INSTALLATION_STATUS.PENDING:
    case INSTALLATION_STATUS.SUPERSEDED:
    case INSTALLATION_STATUS.UNKNOWN:
    default:
      return APP_STATUS.AVAILABLE;
  }
}

/**
 * Calculate health status from readiness and error conditions
 */
export function calculateHealthStatus(
  readyCount: number, 
  totalCount: number, 
  hasErrors = false,
  hasCriticalErrors = false
): HealthStatus {
  if (totalCount === 0) {
    return HEALTH_STATUS.UNKNOWN;
  }
  
  if (hasCriticalErrors) {
    return HEALTH_STATUS.UNHEALTHY;
  }
  
  const readyRatio = readyCount / totalCount;
  
  if (readyRatio === 1.0 && !hasErrors) {
    return HEALTH_STATUS.HEALTHY;
  } else if (readyRatio >= 0.5) {
    return HEALTH_STATUS.DEGRADED;
  } else {
    return HEALTH_STATUS.UNHEALTHY;
  }
}

/**
 * Determine if an app needs attention based on its status and health
 */
export function needsAttention(
  appStatus: AppStatus,
  healthStatus?: HealthStatus,
  hasErrors = false,
  lastUpdateTime?: string
): boolean {
  // Failed states always need attention
  if (appStatus === APP_STATUS.FAILED) {
    return true;
  }
  
  // Health issues need attention
  if (healthStatus === HEALTH_STATUS.UNHEALTHY || healthStatus === HEALTH_STATUS.DEGRADED) {
    return true;
  }
  
  // Errors always need attention
  if (hasErrors) {
    return true;
  }
  
  // Apps stuck in transitional states for too long need attention
  if ([APP_STATUS.INSTALLING, APP_STATUS.UPGRADING, APP_STATUS.UNINSTALLING].includes(appStatus as any)) {
    if (lastUpdateTime) {
      const updateTime = new Date(lastUpdateTime);
      const now = new Date();
      const timeDiff = now.getTime() - updateTime.getTime();
      const hoursDiff = timeDiff / (1000 * 60 * 60);
      
      // If stuck for more than 2 hours, needs attention
      if (hoursDiff > 2) {
        return true;
      }
    }
  }
  
  return false;
}

/**
 * Get display configuration for a status
 */
export function getStatusDisplay(
  type: 'app' | 'installation' | 'health' | 'connection',
  status: string
): StatusDisplayConfig {
  switch (type) {
    case 'app':
      return APP_STATUS_DISPLAY[status as AppStatus] || APP_STATUS_DISPLAY[APP_STATUS.UNKNOWN];
    case 'installation':
      return INSTALLATION_STATUS_DISPLAY[status as InstallationStatus] || INSTALLATION_STATUS_DISPLAY[INSTALLATION_STATUS.UNKNOWN];
    case 'health':
      return HEALTH_STATUS_DISPLAY[status as HealthStatus] || HEALTH_STATUS_DISPLAY[HEALTH_STATUS.UNKNOWN];
    case 'connection':
      return CONNECTION_STATUS_DISPLAY[status as ConnectionStatus] || CONNECTION_STATUS_DISPLAY[CONNECTION_STATUS.ERROR];
    default:
      return APP_STATUS_DISPLAY[APP_STATUS.UNKNOWN];
  }
}

/**
 * Get notification type based on status
 */
export function getNotificationTypeForStatus(
  type: 'app' | 'installation' | 'health' | 'connection',
  status: string
): NotificationType {
  const display = getStatusDisplay(type, status);
  
  if (display.color.includes('error')) {
    return NOTIFICATION_TYPE.ERROR;
  } else if (display.color.includes('warning')) {
    return NOTIFICATION_TYPE.WARNING;
  } else if (display.color.includes('success')) {
    return NOTIFICATION_TYPE.SUCCESS;
  } else {
    return NOTIFICATION_TYPE.INFO;
  }
}

/**
 * Check if status represents a transitional state
 */
export function isTransitionalStatus(status: AppStatus | InstallationStatus): boolean {
  const transitionalStates = [
    APP_STATUS.INSTALLING,
    APP_STATUS.UPGRADING, 
    APP_STATUS.UNINSTALLING,
    INSTALLATION_STATUS.PENDING,
    INSTALLATION_STATUS.INSTALLING,
    INSTALLATION_STATUS.UPGRADING,
    INSTALLATION_STATUS.UNINSTALLING
  ];
  
  return transitionalStates.includes(status as any);
}

/**
 * Check if status represents a successful state
 */
export function isSuccessfulStatus(status: AppStatus | InstallationStatus): boolean {
  return status === 'deployed';
}

/**
 * Check if status represents a failed state
 */
export function isFailedStatus(status: AppStatus | InstallationStatus): boolean {
  return status === 'failed';
}

/**
 * Get available actions based on status
 */
export function getAvailableActionsForStatus(
  appStatus: AppStatus,
  installationStatus?: InstallationStatus
): string[] {
  const actions: string[] = [];
  
  switch (appStatus) {
    case APP_STATUS.AVAILABLE:
      actions.push('install');
      break;
      
    case APP_STATUS.DEPLOYED:
      actions.push('manage', 'upgrade', 'uninstall', 'restart');
      if (installationStatus === INSTALLATION_STATUS.DEPLOYED) {
        actions.push('rollback', 'logs', 'resources');
      }
      break;
      
    case APP_STATUS.FAILED:
      actions.push('retry', 'uninstall', 'logs', 'troubleshoot');
      break;
      
    case APP_STATUS.INSTALLING:
    case APP_STATUS.UPGRADING:
    case APP_STATUS.UNINSTALLING:
      actions.push('cancel', 'logs');
      break;
      
    default:
      actions.push('refresh');
      break;
  }
  
  return actions;
}

/**
 * Format status for display with icon and color
 */
export function formatStatusDisplay(
  type: 'app' | 'installation' | 'health' | 'connection',
  status: string,
  options: {
    showIcon?: boolean;
    showColor?: boolean;
    showDescription?: boolean;
    compact?: boolean;
  } = {}
): string {
  const display = getStatusDisplay(type, status);
  const { showIcon = true, showColor = true, showDescription = false, compact = false } = options;
  
  let result = '';
  
  if (showIcon) {
    result += `<i class="${display.icon}"></i> `;
  }
  
  if (showColor) {
    result += `<span class="${display.color}">`;
  }
  
  result += display.label;
  
  if (showDescription && !compact) {
    result += ` (${display.description})`;
  }
  
  if (showColor) {
    result += '</span>';
  }
  
  return result;
}

/**
 * Get status summary for multiple apps
 */
export function getStatusSummary(statuses: AppStatus[]): {
  total: number;
  healthy: number;
  warning: number;
  error: number;
  unknown: number;
  breakdown: Record<AppStatus, number>;
} {
  const breakdown = statuses.reduce((acc, status) => {
    acc[status] = (acc[status] || 0) + 1;
    return acc;
  }, {} as Record<AppStatus, number>);
  
  const healthy = (breakdown[APP_STATUS.DEPLOYED] || 0);
  const warning = (breakdown[APP_STATUS.INSTALLING] || 0) + 
                   (breakdown[APP_STATUS.UPGRADING] || 0) + 
                   (breakdown[APP_STATUS.UNINSTALLING] || 0);
  const error = (breakdown[APP_STATUS.FAILED] || 0);
  const unknown = (breakdown[APP_STATUS.UNKNOWN] || 0) + 
                   (breakdown[APP_STATUS.AVAILABLE] || 0);
  
  return {
    total: statuses.length,
    healthy,
    warning,
    error,
    unknown,
    breakdown
  };
}

// === Status Badge Component Props Helper ===
export function getStatusBadgeProps(
  type: 'app' | 'installation' | 'health' | 'connection',
  status: string
): {
  color: string;
  label: string;
  tooltip: string;
  icon?: string;
} {
  const display = getStatusDisplay(type, status);
  
  return {
    color: display.color,
    label: display.label,
    tooltip: display.description,
    icon: display.icon
  };
}