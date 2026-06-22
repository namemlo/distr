import {HttpClient} from '@angular/common/http';
import {inject, Injectable} from '@angular/core';
import {Observable} from 'rxjs';
import {
  CreateRunbookRevisionRequest,
  CreateUpdateRunbookRequest,
  Runbook,
  RunbookRevision,
  RunbookSnapshot,
} from '../types/runbook';

const baseUrl = '/api/v1/runbooks';

@Injectable({
  providedIn: 'root',
})
export class RunbooksService {
  private readonly httpClient = inject(HttpClient);

  list(): Observable<Runbook[]> {
    return this.httpClient.get<Runbook[]>(baseUrl);
  }

  get(id: string): Observable<Runbook> {
    return this.httpClient.get<Runbook>(`${baseUrl}/${id}`);
  }

  create(request: CreateUpdateRunbookRequest): Observable<Runbook> {
    return this.httpClient.post<Runbook>(baseUrl, request);
  }

  update(id: string, request: CreateUpdateRunbookRequest): Observable<Runbook> {
    return this.httpClient.put<Runbook>(`${baseUrl}/${id}`, request);
  }

  delete(id: string): Observable<void> {
    return this.httpClient.delete<void>(`${baseUrl}/${id}`);
  }

  listRevisions(runbookId: string): Observable<RunbookRevision[]> {
    return this.httpClient.get<RunbookRevision[]>(`${baseUrl}/${runbookId}/revisions`);
  }

  getRevision(runbookId: string, revisionId: string): Observable<RunbookRevision> {
    return this.httpClient.get<RunbookRevision>(`${baseUrl}/${runbookId}/revisions/${revisionId}`);
  }

  createRevision(runbookId: string, request: CreateRunbookRevisionRequest): Observable<RunbookRevision> {
    return this.httpClient.post<RunbookRevision>(`${baseUrl}/${runbookId}/revisions`, request);
  }

  publishRevision(runbookId: string, revisionId: string): Observable<RunbookSnapshot> {
    return this.httpClient.post<RunbookSnapshot>(`${baseUrl}/${runbookId}/revisions/${revisionId}/publish`, {});
  }
}
