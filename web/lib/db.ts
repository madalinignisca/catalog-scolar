import Dexie, { type Table } from 'dexie';

// ── Cached entities (mirror of server data) ──

export interface CachedGrade {
  id: string;
  serverId?: string;
  studentId: string;
  classId: string;
  subjectId: string;
  semester: 'I' | 'II';
  numericGrade?: number;
  qualifierGrade?: 'FB' | 'B' | 'S' | 'I';
  isThesis: boolean;
  gradeDate: string;
  description?: string;
  teacherId: string;
  updatedAt: string;
  deletedAt?: string;
}

export interface CachedAbsence {
  id: string;
  serverId?: string;
  studentId: string;
  classId: string;
  subjectId: string;
  semester: 'I' | 'II';
  absenceDate: string;
  periodNumber: number;
  absenceType: 'unexcused' | 'medical' | 'excused' | 'school_event';
  excusedBy?: string;
  excusedAt?: string;
  teacherId: string;
  updatedAt: string;
}

// ── Sync queue ──

export interface SyncMutation {
  id?: number;
  clientId: string;
  entityType: 'grade' | 'absence';
  action: 'create' | 'update' | 'delete';
  data: Record<string, unknown>;
  clientTimestamp: string;
  attempts: number;
  lastError?: string;
  status: 'pending' | 'syncing' | 'failed';
  createdAt: string;
}

export interface SyncMeta {
  key: string;
  value: string;
}

// ── Database ──

class CatalogDB extends Dexie {
  grades!: Table<CachedGrade>;
  absences!: Table<CachedAbsence>;
  syncQueue!: Table<SyncMutation>;
  syncMeta!: Table<SyncMeta>;

  constructor() {
    super('catalogro');

    this.version(1).stores({
      grades: 'id, [classId+subjectId+semester], studentId, updatedAt',
      absences: 'id, [classId+absenceDate], studentId, updatedAt',
      syncQueue: '++id, status, entityType, createdAt',
      syncMeta: 'key',
    });
  }
}

export const db = new CatalogDB();
