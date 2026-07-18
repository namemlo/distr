import {HttpClient, HttpParams} from '@angular/common/http';
import {inject, Injectable} from '@angular/core';
import {Observable} from 'rxjs';
import {
  CreateTargetConfigSnapshotRequest,
  TargetConfigSnapshot,
  TargetConfigSnapshotListFilter,
  TargetConfigSnapshotPage,
  TargetConfigSnapshotVerification,
} from '../types/target-config-snapshot';

const baseUrl = '/api/v1/target-config-snapshots/';

@Injectable({providedIn: 'root'})
export class TargetConfigSnapshotsService {
  private readonly httpClient = inject(HttpClient);

  list(filter: TargetConfigSnapshotListFilter = {}): Observable<TargetConfigSnapshotPage> {
    let params = new HttpParams();
    if (filter.deploymentUnitId) params = params.set('deploymentUnitId', filter.deploymentUnitId);
    if (filter.targetEnvironmentAssignmentId) {
      params = params.set('targetEnvironmentAssignmentId', filter.targetEnvironmentAssignmentId);
    }
    if (filter.cursor) params = params.set('cursor', filter.cursor);
    if (filter.limit !== undefined) params = params.set('limit', filter.limit);
    return this.httpClient.get<TargetConfigSnapshotPage>(baseUrl, {params});
  }

  create(request: CreateTargetConfigSnapshotRequest): Observable<TargetConfigSnapshot> {
    return this.httpClient.post<TargetConfigSnapshot>(baseUrl, request);
  }

  get(snapshotId: string): Observable<TargetConfigSnapshot> {
    return this.httpClient.get<TargetConfigSnapshot>(`${baseUrl}${snapshotId}/`);
  }

  verify(snapshotId: string): Observable<TargetConfigSnapshotVerification> {
    return this.httpClient.post<TargetConfigSnapshotVerification>(`${baseUrl}${snapshotId}/verify`, {});
  }
}
