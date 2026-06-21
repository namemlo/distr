import {HttpClient} from '@angular/common/http';
import {inject, Injectable} from '@angular/core';
import {Observable} from 'rxjs';
import {
  ActionDefinition,
  CreateDeploymentProcessRevisionRequest,
  CreateUpdateDeploymentProcessRequest,
  DeploymentProcess,
  DeploymentProcessRevision,
} from '../types/deployment-process';

const baseUrl = '/api/v1/deployment-processes';
const actionDefinitionsUrl = '/api/v1/action-definitions';

@Injectable({
  providedIn: 'root',
})
export class DeploymentProcessesService {
  private readonly httpClient = inject(HttpClient);

  list(): Observable<DeploymentProcess[]> {
    return this.httpClient.get<DeploymentProcess[]>(baseUrl);
  }

  get(id: string): Observable<DeploymentProcess> {
    return this.httpClient.get<DeploymentProcess>(`${baseUrl}/${id}`);
  }

  create(request: CreateUpdateDeploymentProcessRequest): Observable<DeploymentProcess> {
    return this.httpClient.post<DeploymentProcess>(baseUrl, request);
  }

  update(id: string, request: CreateUpdateDeploymentProcessRequest): Observable<DeploymentProcess> {
    return this.httpClient.put<DeploymentProcess>(`${baseUrl}/${id}`, request);
  }

  delete(id: string): Observable<void> {
    return this.httpClient.delete<void>(`${baseUrl}/${id}`);
  }

  listRevisions(processId: string): Observable<DeploymentProcessRevision[]> {
    return this.httpClient.get<DeploymentProcessRevision[]>(`${baseUrl}/${processId}/revisions`);
  }

  getRevision(processId: string, revisionId: string): Observable<DeploymentProcessRevision> {
    return this.httpClient.get<DeploymentProcessRevision>(`${baseUrl}/${processId}/revisions/${revisionId}`);
  }

  createRevision(
    processId: string,
    request: CreateDeploymentProcessRevisionRequest
  ): Observable<DeploymentProcessRevision> {
    return this.httpClient.post<DeploymentProcessRevision>(`${baseUrl}/${processId}/revisions`, request);
  }

  listActionDefinitions(): Observable<ActionDefinition[]> {
    return this.httpClient.get<ActionDefinition[]>(actionDefinitionsUrl);
  }
}
